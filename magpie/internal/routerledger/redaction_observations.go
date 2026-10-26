package routerledger

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	DefaultMaxRedactionObservations = 500
	DefaultRedactionObservationAge  = 7 * 24 * time.Hour
	maxRedactionInputBytes          = 64 << 10
)

const redactionObsTableSQL = `CREATE TABLE IF NOT EXISTS redaction_observations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	request_id TEXT NOT NULL UNIQUE,
	timestamp_ms INTEGER NOT NULL,
	started_at TEXT NOT NULL,
	app_id TEXT NOT NULL,
	app_name TEXT NOT NULL,
	destination TEXT NOT NULL,
	protocol TEXT NOT NULL,
	model TEXT NOT NULL,
	hit INTEGER NOT NULL,
	entity_count INTEGER NOT NULL,
	categories_json TEXT NOT NULL,
	replacements_json TEXT NOT NULL,
	raw_input TEXT NOT NULL,
	redacted_input TEXT NOT NULL,
	cache_hit INTEGER NOT NULL,
	duration_ms INTEGER NOT NULL
)`

type RedactionReplacementItem struct {
	Placeholder string `json:"placeholder"`
	Category    string `json:"category"`
	Raw         string `json:"raw"`
	Count       int    `json:"count"`
}

type RedactionObservation struct {
	ID            int64                      `json:"id"`
	RequestID     string                     `json:"requestId"`
	AppID         string                     `json:"appId"`
	AppName       string                     `json:"appName"`
	Destination   string                     `json:"destination"`
	Protocol      string                     `json:"protocol"`
	Model         string                     `json:"model"`
	StartedAt     string                     `json:"startedAt"`
	DurationMS    int64                      `json:"durationMs"`
	CacheHit      bool                       `json:"cacheHit"`
	Hit           bool                       `json:"hit"`
	EntityCount   int                        `json:"entityCount"`
	Categories    []string                   `json:"categories"`
	RawInput      string                     `json:"rawInput"`
	RedactedInput string                     `json:"redactedInput"`
	Replacements  []RedactionReplacementItem `json:"replacements"`
}

type RedactionObservationOptions struct {
	MaxEntries    int
	MaxAge        time.Duration
	PruneInterval time.Duration
}

type RedactionObservationFilter struct {
	Limit   int
	Before  string
	HitOnly bool
	AppID   string
	Query   string
}

type RedactionObservationSummary struct {
	Total             int            `json:"total"`
	Hits              int            `json:"hits"`
	Passthrough       int            `json:"passthrough"`
	TotalEntities     int            `json:"totalEntities"`
	CategoryBreakdown map[string]int `json:"categoryBreakdown,omitempty"`
}

type RedactionObservationListResult struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Available     bool                        `json:"available"`
	Summary       RedactionObservationSummary `json:"summary"`
	Observations  []RedactionObservation      `json:"observations"`
	NextCursor    string                      `json:"nextCursor,omitempty"`
}

type RedactionObservationStore struct {
	mu     sync.Mutex
	db     *sql.DB
	opts   RedactionObservationOptions
	stopCh chan struct{}
	doneCh chan struct{}
	closed bool
}

func OpenRedactionObservations(path string, optList ...RedactionObservationOptions) (*RedactionObservationStore, error) {
	var opts RedactionObservationOptions
	if len(optList) > 0 {
		opts = optList[0]
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = DefaultMaxRedactionObservations
	}
	if opts.MaxAge <= 0 {
		opts.MaxAge = DefaultRedactionObservationAge
	}
	if opts.PruneInterval <= 0 {
		opts.PruneInterval = min(10*time.Minute, max(25*time.Millisecond, opts.MaxAge/2))
	}
	if !filepath.IsAbs(path) || opts.MaxEntries > 10000 || opts.MaxAge > 365*24*time.Hour {
		return nil, ErrInvalid
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnavailable
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		f, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if createErr != nil {
			return nil, ErrUnavailable
		}
		if closeErr := f.Close(); closeErr != nil {
			return nil, ErrUnavailable
		}
	} else if err != nil {
		return nil, ErrUnavailable
	} else if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnavailable
	} else if info.Size() > 64<<20 {
		return nil, ErrCapacity
	}
	_ = os.Chmod(path, 0600)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, ErrUnavailable
	}
	db.SetMaxOpenConns(1)
	s := &RedactionObservationStore{
		db:     db,
		opts:   opts,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	go s.janitor()
	return s, nil
}

func (s *RedactionObservationStore) janitor() {
	defer close(s.doneCh)
	ticker := time.NewTicker(s.opts.PruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			_ = s.Prune(now.UTC())
		}
	}
}

func (s *RedactionObservationStore) Prune(now time.Time) error {
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnavailable
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	if err := s.pruneTx(tx, now.UTC(), 0); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *RedactionObservationStore) init() error {
	for _, stmt := range []string{
		"PRAGMA busy_timeout=3000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA journal_mode=DELETE",
		"PRAGMA secure_delete=ON",
		"PRAGMA max_page_count=16384",
		redactionObsTableSQL,
		"CREATE INDEX IF NOT EXISTS idx_redaction_obs_order ON redaction_observations(timestamp_ms DESC, id DESC)",
		"CREATE INDEX IF NOT EXISTS idx_redaction_obs_hit ON redaction_observations(hit, timestamp_ms DESC)",
		"CREATE INDEX IF NOT EXISTS idx_redaction_obs_app ON redaction_observations(app_id, timestamp_ms DESC)",
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return ErrUnavailable
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	if err := s.pruneTx(tx, time.Now().UTC(), 0); err != nil {
		return err
	}
	return tx.Commit()
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

func (s *RedactionObservationStore) pruneTx(tx *sql.Tx, now time.Time, reserve int) error {
	cutoff := now.Add(-s.opts.MaxAge).UnixMilli()
	if _, err := tx.Exec("DELETE FROM redaction_observations WHERE timestamp_ms < ?", cutoff); err != nil {
		return ErrUnavailable
	}
	var count int
	if err := tx.QueryRow("SELECT count(*) FROM redaction_observations").Scan(&count); err != nil {
		return ErrUnavailable
	}
	remove := count + reserve - s.opts.MaxEntries
	if remove <= 0 {
		return nil
	}
	if _, err := tx.Exec(
		"DELETE FROM redaction_observations WHERE id IN (SELECT id FROM redaction_observations ORDER BY timestamp_ms ASC, id ASC LIMIT ?)",
		remove,
	); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *RedactionObservationStore) Record(rec RedactionObservation) error {
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnavailable
	}
	rec.RequestID = strings.TrimSpace(rec.RequestID)
	rec.AppID = strings.TrimSpace(rec.AppID)
	if rec.RequestID == "" || len(rec.RequestID) > 128 || rec.AppID == "" || len(rec.AppID) > 64 {
		return ErrInvalid
	}
	now := time.Now().UTC()
	var ts time.Time
	if rec.StartedAt != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, rec.StartedAt); err == nil {
			ts = parsed.UTC()
		}
	}
	if ts.IsZero() {
		ts = now
		rec.StartedAt = ts.Format(time.RFC3339Nano)
	}
	timestampMS := ts.UnixMilli()
	if rec.Categories == nil {
		rec.Categories = []string{}
	}
	if rec.Replacements == nil {
		rec.Replacements = []RedactionReplacementItem{}
	}
	rec.RawInput = truncateUTF8(rec.RawInput, maxRedactionInputBytes)
	rec.RedactedInput = truncateUTF8(rec.RedactedInput, maxRedactionInputBytes)
	if rec.DurationMS < 0 {
		rec.DurationMS = 0
	}
	if rec.EntityCount < 0 {
		rec.EntityCount = 0
	}
	categoriesJSON, err := json.Marshal(rec.Categories)
	if err != nil {
		return ErrInvalid
	}
	replacementsJSON, err := json.Marshal(rec.Replacements)
	if err != nil {
		return ErrInvalid
	}
	hitInt := 0
	if rec.Hit {
		hitInt = 1
	}
	cacheHitInt := 0
	if rec.CacheHit {
		cacheHitInt = 1
	}

	tx, err := s.db.Begin()
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM redaction_observations WHERE request_id = ?)", rec.RequestID).Scan(&exists); err != nil {
		return ErrUnavailable
	}
	reserve := 1
	if exists {
		reserve = 0
	}
	if err := s.pruneTx(tx, now, reserve); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO redaction_observations (
		request_id, timestamp_ms, started_at, app_id, app_name, destination, protocol, model,
		hit, entity_count, categories_json, replacements_json, raw_input, redacted_input,
		cache_hit, duration_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(request_id) DO UPDATE SET
		timestamp_ms = excluded.timestamp_ms,
		started_at = excluded.started_at,
		app_id = excluded.app_id,
		app_name = excluded.app_name,
		destination = excluded.destination,
		protocol = excluded.protocol,
		model = excluded.model,
		hit = excluded.hit,
		entity_count = excluded.entity_count,
		categories_json = excluded.categories_json,
		replacements_json = excluded.replacements_json,
		raw_input = excluded.raw_input,
		redacted_input = excluded.redacted_input,
		cache_hit = excluded.cache_hit,
		duration_ms = excluded.duration_ms`,
		rec.RequestID, timestampMS, rec.StartedAt, rec.AppID, rec.AppName, rec.Destination, rec.Protocol, rec.Model,
		hitInt, rec.EntityCount, string(categoriesJSON), string(replacementsJSON), rec.RawInput, rec.RedactedInput,
		cacheHitInt, rec.DurationMS,
	)
	if err != nil {
		return ErrUnavailable
	}
	if err := tx.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *RedactionObservationStore) Query(q RedactionObservationFilter) (RedactionObservationListResult, error) {
	if s == nil {
		return RedactionObservationListResult{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return RedactionObservationListResult{}, ErrUnavailable
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	summary := RedactionObservationSummary{
		CategoryBreakdown: map[string]int{},
	}

	cutoff := time.Now().UTC().Add(-s.opts.MaxAge).UnixMilli()
	if _, err := s.db.Exec("DELETE FROM redaction_observations WHERE timestamp_ms < ?", cutoff); err != nil {
		return RedactionObservationListResult{}, ErrUnavailable
	}
	var totalCount, hitCount, totalEntities sql.NullInt64
	if err := s.db.QueryRow(
		"SELECT count(*), COALESCE(SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END), 0), COALESCE(SUM(entity_count), 0) FROM redaction_observations WHERE timestamp_ms >= ?",
		cutoff,
	).Scan(&totalCount, &hitCount, &totalEntities); err != nil {
		return RedactionObservationListResult{}, ErrUnavailable
	}
	summary.Total = int(totalCount.Int64)
	summary.Hits = int(hitCount.Int64)
	summary.Passthrough = summary.Total - summary.Hits
	summary.TotalEntities = int(totalEntities.Int64)

	var where []string
	var args []any
	where = append(where, "timestamp_ms >= ?")
	args = append(args, cutoff)

	if q.HitOnly {
		where = append(where, "hit = 1")
	}
	if appID := strings.TrimSpace(q.AppID); appID != "" {
		where = append(where, "app_id = ?")
		args = append(args, appID)
	}
	if search := strings.TrimSpace(q.Query); search != "" {
		pattern := "%" + strings.ToLower(search) + "%"
		where = append(where, "(LOWER(raw_input) LIKE ? OR LOWER(redacted_input) LIKE ? OR LOWER(categories_json) LIKE ? OR LOWER(replacements_json) LIKE ? OR LOWER(model) LIKE ? OR LOWER(app_id) LIKE ?)")
		args = append(args, pattern, pattern, pattern, pattern, pattern, pattern)
	}
	if cursor := strings.TrimSpace(q.Before); cursor != "" {
		parts := strings.SplitN(cursor, ":", 2)
		if len(parts) != 2 || parts[1] == "" {
			return RedactionObservationListResult{}, ErrInvalid
		}
		cursorMS, parseErr := strconv.ParseInt(parts[0], 10, 64)
		cursorID, idErr := strconv.ParseInt(parts[1], 10, 64)
		if parseErr != nil || idErr != nil {
			return RedactionObservationListResult{}, ErrInvalid
		}
		where = append(where, "(timestamp_ms < ? OR (timestamp_ms = ? AND id < ?))")
		args = append(args, cursorMS, cursorMS, cursorID)
	}

	querySQL := "SELECT id, request_id, timestamp_ms, started_at, app_id, app_name, destination, protocol, model, hit, entity_count, categories_json, replacements_json, raw_input, redacted_input, cache_hit, duration_ms FROM redaction_observations WHERE " +
		strings.Join(where, " AND ") +
		" ORDER BY timestamp_ms DESC, id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := s.db.Query(querySQL, args...)
	if err != nil {
		return RedactionObservationListResult{}, ErrUnavailable
	}
	defer rows.Close()

	items := make([]RedactionObservation, 0, limit)
	var lastTS int64
	var nextCursor string
	for rows.Next() {
		var rec RedactionObservation
		var tsMS int64
		var hitInt, cacheHitInt int
		var categoriesRaw, replacementsRaw string
		if err := rows.Scan(
			&rec.ID, &rec.RequestID, &tsMS, &rec.StartedAt, &rec.AppID, &rec.AppName, &rec.Destination, &rec.Protocol, &rec.Model,
			&hitInt, &rec.EntityCount, &categoriesRaw, &replacementsRaw, &rec.RawInput, &rec.RedactedInput,
			&cacheHitInt, &rec.DurationMS,
		); err != nil {
			return RedactionObservationListResult{}, ErrUnavailable
		}
		rec.Hit = hitInt != 0
		rec.CacheHit = cacheHitInt != 0
		if json.Unmarshal([]byte(categoriesRaw), &rec.Categories) != nil || rec.Categories == nil {
			rec.Categories = []string{}
		}
		if json.Unmarshal([]byte(replacementsRaw), &rec.Replacements) != nil || rec.Replacements == nil {
			rec.Replacements = []RedactionReplacementItem{}
		}
		if len(items) == limit {
			last := items[len(items)-1]
			nextCursor = fmt.Sprintf("%d:%d", lastTS, last.ID)
			break
		}
		lastTS = tsMS
		items = append(items, rec)
	}
	if err := rows.Err(); err != nil {
		return RedactionObservationListResult{}, ErrUnavailable
	}
	return RedactionObservationListResult{
		SchemaVersion: 1,
		Available:     true,
		Summary:       summary,
		Observations:  items,
		NextCursor:    nextCursor,
	}, nil
}

func (s *RedactionObservationStore) Clear() error {
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnavailable
	}
	if _, err := s.db.Exec("DELETE FROM redaction_observations"); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *RedactionObservationStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	stopCh := s.stopCh
	doneCh := s.doneCh
	s.mu.Unlock()
	if stopCh != nil {
		close(stopCh)
	}
	if doneCh != nil {
		<-doneCh
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}
