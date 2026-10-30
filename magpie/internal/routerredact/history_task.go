package routerredact

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/cloudcodec"
	"github.com/yetone/magpie/internal/routercloud"
)

type preparationPart struct {
	field, offset int
	text          string
}
type preparationMessage struct {
	texts    []string
	parts    []preparationPart
	next     int
	entities []Entity
}

// HistoryTask retains selected user text in memory only. A restart requires
// resubmission of that text; completed records survive independently.
type HistoryTask struct {
	mu              sync.Mutex
	controlMu       sync.Mutex
	canceled        bool
	activeCancel    context.CancelFunc
	snapshot        atomic.Value
	scope, material string
	versions        Versions
	messages        []preparationMessage
	progress        HistoryProgress
}
type HistoryProgress struct {
	TotalMessages     int  `json:"totalMessages"`
	CompletedMessages int  `json:"completedMessages"`
	TotalCharacters   int  `json:"totalCharacters"`
	CompletedChunks   int  `json:"completedChunks"`
	TotalChunks       int  `json:"totalChunks"`
	Done              bool `json:"done"`
}

func (p *Pipeline) NewHistoryTask(ctx context.Context, in Input, selected []int) (*HistoryTask, error) {
	if p.options.Results == nil || in.AuthenticatedScope == "" || in.RecognitionMaterial == "" || len(selected) < 1 || len(selected) > 100 {
		return nil, fail("redaction_invalid_input")
	}
	if routercloud.ValidateInput(string(in.Protocol), in.Body) != nil {
		return nil, fail("wire_invalid")
	}
	view, err := cloudcodec.NewUserHistoryViewForApp(string(in.Protocol), in.Body, in.AppID)
	if err != nil {
		return nil, fail("wire_invalid")
	}
	all := view.Messages()
	task := &HistoryTask{scope: in.AuthenticatedScope, material: in.RecognitionMaterial, versions: in.Versions}
	seen := map[int]bool{}
	for _, ordinal := range selected {
		if seen[ordinal] {
			return nil, fail("redaction_invalid_input")
		}
		seen[ordinal] = true
		var found *cloudcodec.UserMessage
		for i := range all {
			if all[i].Ordinal == ordinal {
				found = &all[i]
				break
			}
		}
		if found == nil {
			return nil, fail("redaction_invalid_input")
		}
		m := preparationMessage{texts: found.Texts}
		for field, text := range m.texts {
			task.progress.TotalCharacters += utf8.RuneCountInString(text)
			for _, chunk := range ChunkText(text, DefaultLLMMaxChunkChars, DefaultLLMChunkOverlap) {
				m.parts = append(m.parts, preparationPart{field, chunk.ByteOffset, chunk.Text})
			}
		}
		if task.progress.TotalCharacters > 200000 {
			return nil, fail("redaction_input_limit")
		}
		task.progress.TotalMessages++
		task.progress.TotalChunks += len(m.parts)
		cached, e := p.options.Results.Load(ctx, task.scope, task.material, [][]string{m.texts})
		if e != nil {
			return nil, e
		}
		if cached[0] != nil {
			task.progress.CompletedMessages++
			task.progress.CompletedChunks += len(m.parts)
		} else {
			task.messages = append(task.messages, m)
		}
	}
	task.progress.Done = len(task.messages) == 0
	task.snapshot.Store(task.progress)
	return task, nil
}
func (t *HistoryTask) Progress() HistoryProgress {
	if p := t.snapshot.Load(); p != nil {
		return p.(HistoryProgress)
	}
	return HistoryProgress{}
}
func (t *HistoryTask) Cancel() {
	t.controlMu.Lock()
	defer t.controlMu.Unlock()
	t.canceled = true
	if t.activeCancel != nil {
		t.activeCancel()
	}
}

// Step executes at most one existing strict batch, then releases the runtime
// queue. The UI explicitly requests the next step, so pause needs no worker.
func (t *HistoryTask) Step(parent context.Context, p *Pipeline) (HistoryProgress, error) {
	if !t.mu.TryLock() {
		return HistoryProgress{}, fail("redaction_history_task_busy")
	}
	defer t.mu.Unlock()
	defer func() { t.snapshot.Store(t.progress) }()
	if t.progress.Done {
		return t.progress, nil
	}
	ctx, cancel := context.WithTimeout(parent, p.options.Budget)
	defer cancel()
	t.controlMu.Lock()
	if t.canceled {
		t.controlMu.Unlock()
		return t.progress, fail("redaction_canceled")
	}
	t.activeCancel = cancel
	t.controlMu.Unlock()
	defer func() { t.controlMu.Lock(); t.activeCancel = nil; t.controlMu.Unlock() }()
	m := &t.messages[0]
	cached, err := p.options.Results.Load(ctx, t.scope, t.material, [][]string{m.texts})
	if err != nil {
		return t.progress, err
	}
	if cached[0] != nil {
		t.progress.CompletedChunks += len(m.parts) - m.next
		m.next = len(m.parts)
	} else {
		end, runes := m.next, 0
		batch := Extraction{Versions: t.versions}
		for end < len(m.parts) && end-m.next < PrivacyMaxBatchFields {
			n := utf8.RuneCountInString(m.parts[end].text)
			if end > m.next && runes+n > PrivacyMaxBatchRunes {
				break
			}
			runes += n
			batch.Texts = append(batch.Texts, m.parts[end].text)
			end++
		}
		if len(batch.Texts) > 0 {
			if p.options.Extractor == nil {
				return t.progress, fail("redaction_unavailable")
			}
			result, e := p.options.Extractor.Extract(ctx, batch)
			if e != nil {
				return t.progress, e
			}
			entities, e := validateEntities(batch.Texts, result)
			if e != nil {
				return t.progress, e
			}
			if e = contextError(ctx); e != nil {
				return t.progress, e
			}
			for _, entity := range entities {
				part := m.parts[m.next+entity.Field]
				entity.Field = part.field
				entity.Start += part.offset
				entity.End += part.offset
				m.entities = append(m.entities, entity)
			}
			t.progress.CompletedChunks += end - m.next
			m.next = end
		}
		if m.next == len(m.parts) {
			// Whole-field deterministic rules retain the same coverage as normal
			// recognition without sending whole history to the model.
			for field, text := range m.texts {
				spans, e := DefaultRuleDetector().DetectContext(ctx, text)
				if e != nil {
					return t.progress, e
				}
				spans = append(spans, DefaultDictionaryDetector().Detect(text)...)
				for _, span := range spans {
					category := span.CanonicalLabel
					if category == "" {
						category = CanonicalizeLabel(span.Label)
					}
					if !AllowedPassThroughLabels[category] {
						m.entities = append(m.entities, Entity{field, span.Start, span.End, span.Value, category})
					}
				}
			}
			entities := mergeHistoryEntities(m.texts, m.entities)
			if err = p.options.Results.Save(ctx, t.scope, t.material, m.texts, Result{Complete: true, Entities: entities}); err != nil {
				return t.progress, err
			}
		}
	}
	if m.next == len(m.parts) {
		t.progress.CompletedMessages++
		t.messages[0] = preparationMessage{}
		t.messages = t.messages[1:]
	}
	t.progress.Done = len(t.messages) == 0
	return t.progress, nil
}
func mergeHistoryEntities(texts []string, entities []Entity) []Entity {
	sort.Slice(entities, func(i, j int) bool {
		a, b := entities[i], entities[j]
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.End != b.End {
			return a.End > b.End
		}
		return a.Category < b.Category
	})
	var out []Entity
	for _, e := range entities {
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.Field == e.Field && e.Start < last.End {
				if e.Start == last.Start && e.End == last.End && last.Category == CanonicalizeLabel(e.Category) {
					last.Category = e.Category
				}
				if e.End > last.End {
					last.End = e.End
					last.Text = texts[e.Field][last.Start:last.End]
				}
				continue
			}
		}
		out = append(out, e)
	}
	return out
}
