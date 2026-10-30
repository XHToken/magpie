package routerredact

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/yetone/magpie/internal/dirs"
	_ "modernc.org/sqlite"
)

const RecognitionProjectionVersion = "actual-user-messages-utf8-blocks-v1"
const resultSchema = 1

// ResultStore is reliable private evidence, not a TTL cache. It retains no full
// source messages and never grants response restoration by itself.
type ResultStore struct {
	db      *sql.DB
	dir, id string
	index   []byte
	data    cipher.AEAD
	owner   *os.File
	mu      sync.Mutex
	gates   map[string]*resultGate
}
type resultGate struct {
	slot chan struct{}
	refs int
}
type resultKeyBundle struct {
	Version int
	StoreID string
	Master  []byte
}
type storedRecognition struct {
	Schema                                            int
	StoreID, Scope, Material, Fingerprint, Projection string
	Revision                                          int
	Complete                                          bool
	BlockLengths                                      []int
	Coverage                                          []int
	Entities                                          []recognitionEntity
}

// Only the encrypted payload uses this wire type. Entity's public JSON
// marshaler deliberately hides sensitive coordinates and originals.
type recognitionEntity Entity

func deriveResultKey(master []byte, purpose string) []byte {
	m := hmac.New(sha256.New, master)
	m.Write([]byte("wefto-redaction-results-v1/" + purpose))
	return m.Sum(nil)
}

// OpenResultStore must be called by the layout's service owner. A separate file
// lock additionally excludes offline or accidental second writers.
func OpenResultStore(l dirs.Layout) (_ *ResultStore, err error) {
	return OpenResultStoreWithKeyProtector(l, nativeResultKeyProtector{})
}

// ResultKeyProtector is a trusted embedding dependency, never request input.
// Integration tests supply an isolated protector instead of touching a user's
// login keychain. Ordinary services always use OpenResultStore's OS protector.
type ResultKeyProtector interface {
	ProtectResultKey([]byte) ([]byte, error)
	UnprotectResultKey([]byte) ([]byte, error)
}

type nativeResultKeyProtector struct{}

func (nativeResultKeyProtector) ProtectResultKey(raw []byte) ([]byte, error) {
	return protectResultKey(raw)
}
func (nativeResultKeyProtector) UnprotectResultKey(raw []byte) ([]byte, error) {
	return unprotectResultKey(raw)
}

func OpenResultStoreWithKeyProtector(l dirs.Layout, keys ResultKeyProtector) (_ *ResultStore, err error) {
	if keys == nil {
		return nil, fail("redaction_store_key_unavailable")
	}
	dir := l.RedactionResults()
	if !filepath.IsAbs(l.State) {
		return nil, fail("redaction_store_permissions")
	}
	if e := checkResultPath(l.State); e != nil {
		return nil, e
	}
	created := false
	if _, e := os.Lstat(dir); errors.Is(e, os.ErrNotExist) {
		if e = os.Mkdir(dir, 0700); e != nil {
			return nil, fail("redaction_store_unavailable")
		}
		created = true
		if e = secureResultDirectory(dir); e != nil {
			return nil, e
		}
	} else if e != nil {
		return nil, fail("redaction_store_unavailable")
	}
	if checkResultPath(dir) != nil || verifyResultPrivate(dir) != nil {
		return nil, fail("redaction_store_permissions")
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		return nil, fail("redaction_store_unavailable")
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "owner.lock", resultKeyFile(), "results.sqlite", "results.sqlite-journal":
		default:
			return nil, fail("redaction_store_corrupt")
		}
		if entry.IsDir() {
			return nil, fail("redaction_store_permissions")
		}
		p := filepath.Join(dir, entry.Name())
		if checkResultPath(p) != nil || verifyResultPrivate(p) != nil {
			return nil, fail("redaction_store_permissions")
		}
	}
	// A failed first keychain connection leaves no key or database. Retrying
	// only that unambiguous empty state is safe; partial key/DB files stay intact.
	if len(entries) == 0 || (len(entries) == 1 && entries[0].Name() == "owner.lock") {
		created = true
	}
	owner, e := lockResultStore(filepath.Join(dir, "owner.lock"))
	if e != nil {
		return nil, e
	}
	s := &ResultStore{dir: dir, owner: owner, gates: map[string]*resultGate{}}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	keyPath, dbPath := filepath.Join(dir, resultKeyFile()), filepath.Join(dir, "results.sqlite")
	var bundle resultKeyBundle
	if created {
		master := make([]byte, 32)
		id := make([]byte, 16)
		if _, e = rand.Read(master); e != nil {
			return nil, fail("redaction_store_key_unavailable")
		}
		if _, e = rand.Read(id); e != nil {
			return nil, fail("redaction_store_key_unavailable")
		}
		bundle = resultKeyBundle{resultSchema, hex.EncodeToString(id), master}
		raw, _ := json.Marshal(bundle)
		wrapped, e := keys.ProtectResultKey(raw)
		if e != nil {
			return nil, e
		}
		f, e := os.OpenFile(keyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, fail("redaction_store_key_unavailable")
		}
		_, e = f.Write(wrapped)
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil || ce != nil {
			return nil, fail("redaction_store_key_unavailable")
		}
		f, e = os.OpenFile(dbPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if e != nil {
			return nil, fail("redaction_store_unavailable")
		}
		if e = f.Close(); e != nil {
			return nil, fail("redaction_store_unavailable")
		}
	} else {
		info, e := os.Lstat(dbPath)
		if e != nil || info.Size() == 0 {
			return nil, fail("redaction_store_corrupt")
		}
		if info.Size() > 256<<20 {
			return nil, fail("redaction_store_capacity")
		}
		raw, e := os.ReadFile(keyPath)
		if e != nil || len(raw) > 16384 {
			return nil, fail("redaction_store_key_unavailable")
		}
		unwrapped, e := keys.UnprotectResultKey(raw)
		if e != nil {
			return nil, e
		}
		if json.Unmarshal(unwrapped, &bundle) != nil {
			return nil, fail("redaction_store_key_unavailable")
		}
	}
	if bundle.Version != resultSchema || len(bundle.Master) != 32 || len(bundle.StoreID) != 32 {
		return nil, fail("redaction_store_key_unavailable")
	}
	if verifyResultPrivate(keyPath) != nil || verifyResultPrivate(dbPath) != nil {
		return nil, fail("redaction_store_permissions")
	}
	s.id = bundle.StoreID
	s.index = deriveResultKey(bundle.Master, "index")
	block, e := aes.NewCipher(deriveResultKey(bundle.Master, "data"))
	if e != nil {
		return nil, fail("redaction_store_key_unavailable")
	}
	s.data, e = cipher.NewGCM(block)
	if e != nil {
		return nil, fail("redaction_store_key_unavailable")
	}
	s.db, e = sql.Open("sqlite", dbPath)
	if e != nil {
		return nil, fail("redaction_store_unavailable")
	}
	s.db.SetMaxOpenConns(1)
	var check string
	if e = s.db.QueryRow("PRAGMA quick_check").Scan(&check); e != nil || check != "ok" {
		return nil, fail("redaction_store_corrupt")
	}
	if !created {
		var pageSize int
		if s.db.QueryRow("PRAGMA page_size").Scan(&pageSize) != nil || pageSize != 4096 {
			return nil, fail("redaction_store_schema")
		}
		var schema int
		if s.db.QueryRow("PRAGMA user_version").Scan(&schema) != nil || schema != resultSchema {
			return nil, fail("redaction_store_schema")
		}
		var id, proof string
		if s.db.QueryRow("SELECT store_id,proof FROM identity").Scan(&id, &proof) != nil || id != s.id || !hmac.Equal([]byte(proof), []byte(s.digest("identity", []byte(s.id)))) {
			return nil, fail("redaction_store_corrupt")
		}
		var extra int
		if s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type IN ('view','trigger') OR (type='table' AND name NOT IN ('identity','results'))").Scan(&extra) != nil || extra != 0 {
			return nil, fail("redaction_store_schema")
		}
	}
	for _, q := range []string{"PRAGMA busy_timeout=2000", "PRAGMA journal_mode=DELETE", "PRAGMA synchronous=EXTRA", "PRAGMA foreign_keys=ON", "PRAGMA secure_delete=ON", "PRAGMA max_page_count=65536"} {
		if _, e = s.db.Exec(q); e != nil {
			return nil, fail("redaction_store_unavailable")
		}
	}
	if created {
		tx, e := s.db.Begin()
		if e != nil {
			return nil, fail("redaction_store_unavailable")
		}
		defer tx.Rollback()
		for _, q := range []string{"CREATE TABLE identity (store_id TEXT PRIMARY KEY, proof TEXT NOT NULL)", "CREATE TABLE results (result_key TEXT PRIMARY KEY, payload BLOB NOT NULL)", "PRAGMA user_version=1"} {
			if _, e = tx.Exec(q); e != nil {
				return nil, fail("redaction_store_unavailable")
			}
		}
		if _, e = tx.Exec("INSERT INTO identity VALUES (?,?)", s.id, s.digest("identity", []byte(s.id))); e != nil {
			return nil, fail("redaction_store_unavailable")
		}
		if e = tx.Commit(); e != nil {
			return nil, fail("redaction_store_unavailable")
		}
	}
	return s, nil
}

func (s *ResultStore) digest(purpose string, raw []byte) string {
	m := hmac.New(sha256.New, s.index)
	m.Write([]byte(purpose))
	m.Write([]byte{0})
	m.Write(raw)
	return hex.EncodeToString(m.Sum(nil))
}
func (s *ResultStore) record(scope, material string, texts []string) (string, storedRecognition) {
	// JSON arrays provide unambiguous types, ordering and exact UTF-8 boundaries.
	raw, _ := json.Marshal(struct {
		Version string
		Blocks  []string
	}{RecognitionProjectionVersion, texts})
	fp := s.digest("content", raw)
	keyRaw, _ := json.Marshal([]string{s.id, scope, RecognitionProjectionVersion, material, fp})
	r := storedRecognition{Schema: resultSchema, StoreID: s.id, Scope: scope, Material: material, Fingerprint: fp, Projection: RecognitionProjectionVersion, Revision: 1, Complete: true}
	for _, text := range texts {
		r.BlockLengths = append(r.BlockLengths, len(text))
		r.Coverage = append(r.Coverage, len(text))
	}
	return s.digest("result", keyRaw), r
}
func (s *ResultStore) aad(key string) []byte {
	return []byte("wefto-results/schema1/key1/revision1/complete/" + s.id + "/" + key)
}

// Load reads only this request's keys and authenticates even zero-hit records.
func (s *ResultStore) Load(ctx context.Context, scope, material string, messages [][]string) ([]*Result, error) {
	if s == nil {
		return nil, fail("redaction_store_unavailable")
	}
	out := make([]*Result, len(messages))
	if len(messages) == 0 {
		return out, nil
	}
	args := make([]any, len(messages))
	positions := map[string][]int{}
	query := "SELECT result_key,payload FROM results WHERE result_key IN ("
	for i, texts := range messages {
		key, _ := s.record(scope, material, texts)
		args[i] = key
		positions[key] = append(positions[key], i)
		if i > 0 {
			query += ","
		}
		query += "?"
	}
	query += ")"
	rows, e := s.db.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, fail("redaction_store_unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var payload []byte
		if rows.Scan(&key, &payload) != nil || len(payload) < s.data.NonceSize() || len(payload) > 32<<20 {
			return nil, fail("redaction_store_corrupt")
		}
		raw, e := s.data.Open(nil, payload[:s.data.NonceSize()], payload[s.data.NonceSize():], s.aad(key))
		if e != nil {
			return nil, fail("redaction_store_corrupt")
		}
		var stored storedRecognition
		if json.Unmarshal(raw, &stored) != nil {
			return nil, fail("redaction_store_corrupt")
		}
		for _, i := range positions[key] {
			_, expected := s.record(scope, material, messages[i])
			entities := stored.Entities
			stored.Entities = nil
			a, _ := json.Marshal(stored)
			b, _ := json.Marshal(expected)
			stored.Entities = entities
			if !hmac.Equal(a, b) {
				return nil, fail("redaction_store_corrupt")
			}
			decoded := make([]Entity, len(entities))
			for j, e := range entities {
				decoded[j] = Entity(e)
			}
			valid, e := validateEntities(messages[i], Result{Complete: true, Entities: decoded})
			if e != nil {
				return nil, fail("redaction_store_corrupt")
			}
			out[i] = &Result{Complete: true, Entities: valid}
		}
	}
	if rows.Err() != nil {
		return nil, fail("redaction_store_unavailable")
	}
	return out, nil
}
func (s *ResultStore) Save(ctx context.Context, scope, material string, texts []string, result Result) error {
	entities, e := validateEntities(texts, result)
	if e != nil {
		return e
	}
	key, record := s.record(scope, material, texts)
	for _, entity := range entities {
		record.Entities = append(record.Entities, recognitionEntity(entity))
	}
	raw, _ := json.Marshal(record)
	nonce := make([]byte, s.data.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return fail("redaction_store_unavailable")
	}
	payload := append(nonce, s.data.Seal(nil, nonce, raw, s.aad(key))...)
	if _, e = s.db.ExecContext(ctx, "INSERT INTO results VALUES (?,?) ON CONFLICT(result_key) DO NOTHING", key, payload); e != nil {
		return fail("redaction_store_commit_failed")
	}
	// A concurrent winner must authenticate too; never silently accept a bad row.
	_, e = s.Load(ctx, scope, material, [][]string{texts})
	return e
}
func (s *ResultStore) acquire(ctx context.Context, key string) (func(), error) {
	s.mu.Lock()
	g := s.gates[key]
	if g == nil {
		if len(s.gates) >= 128 {
			s.mu.Unlock()
			return nil, fail("redaction_input_limit")
		}
		g = &resultGate{slot: make(chan struct{}, 1)}
		s.gates[key] = g
	}
	g.refs++
	s.mu.Unlock()
	drop := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		g.refs--
		if g.refs == 0 {
			delete(s.gates, key)
		}
	}
	select {
	case g.slot <- struct{}{}:
		return func() { <-g.slot; drop() }, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}
func (s *ResultStore) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.db != nil {
		err = s.db.Close()
	}
	if s.owner != nil {
		s.owner.Close()
	}
	return err
}
