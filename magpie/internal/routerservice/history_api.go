package routerservice

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerredact"
)

type historyPreparation struct {
	task                 *routerredact.HistoryTask
	app, scope, material string
	created              time.Time
}

// The authenticated management caller supplies the original protocol body and
// explicitly selected user ordinals. No observation row is trusted as evidence.
func historyPreparationAPI(w http.ResponseWriter, req *http.Request, config *routerconfig.Store, r *RedactionService) {
	reject := func(status int, code string) {
		providerJSON(w, status, map[string]any{"error": map[string]string{"code": code}})
	}
	if r == nil || config == nil || r.results == nil || r.resultsError != nil {
		reject(503, "redaction_store_unavailable")
		return
	}
	if req.URL.RawQuery != "" {
		reject(400, "invalid_query")
		return
	}
	var input struct {
		Action, ID, AppID, Protocol string
		Body                        json.RawMessage
		Selected                    []int
	}
	req.Body = http.MaxBytesReader(w, req.Body, 8<<20)
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || !routerconfig.ValidAppID(input.AppID) {
		reject(400, "invalid_request")
		return
	}
	apps, err := config.AppsContext(req.Context())
	if err != nil {
		reject(503, "configuration_unavailable")
		return
	}
	var profile *routerconfig.AppProfile
	for i := range apps {
		if apps[i].ID == input.AppID && apps[i].Configured {
			profile = &apps[i]
			break
		}
	}
	if profile == nil {
		reject(409, "app_not_registered")
		return
	}
	r.mu.Lock()
	p, s, old, err := r.currentLocked(req.Context())
	if err == nil {
		r.refs[p]++
	}
	r.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if err != nil {
		reject(503, "redaction_unavailable")
		return
	}
	defer r.release(p)
	scope, material := profile.RecognitionScope(), s.RecognitionMaterialID()
	r.historyMu.Lock()
	if r.ctx.Err() != nil {
		r.historyMu.Unlock()
		reject(503, "redaction_unavailable")
		return
	}
	if r.historyTasks == nil {
		r.historyTasks = map[string]*historyPreparation{}
	}
	for id, task := range r.historyTasks {
		if time.Since(task.created) > time.Hour {
			task.task.Cancel()
			delete(r.historyTasks, id)
		}
	}
	task := r.historyTasks[input.ID]
	r.historyMu.Unlock()
	if input.Action == "create" {
		preparation, e := p.NewHistoryTask(req.Context(), routerredact.Input{AppID: input.AppID, AuthenticatedScope: scope, RecognitionMaterial: material, Protocol: routerredact.Protocol(input.Protocol), Versions: s.Versions(), Body: input.Body}, input.Selected)
		if e != nil {
			reject(409, routerredact.SafeFailureCode(e.Error()))
			return
		}
		random := make([]byte, 16)
		if _, e = rand.Read(random); e != nil {
			reject(503, "redaction_unavailable")
			return
		}
		input.ID = hex.EncodeToString(random)
		task = &historyPreparation{preparation, input.AppID, scope, material, time.Now()}
		r.historyMu.Lock()
		if r.ctx.Err() != nil || r.historyTasks == nil {
			r.historyMu.Unlock()
			task.task.Cancel()
			reject(503, "redaction_unavailable")
			return
		}
		if len(r.historyTasks) >= 8 {
			r.historyMu.Unlock()
			reject(409, "redaction_history_task_limit")
			return
		}
		r.historyTasks[input.ID] = task
		r.historyMu.Unlock()
	} else if task == nil || task.app != input.AppID || task.scope != scope || task.material != material {
		reject(409, "redaction_history_prepare_required")
		return
	}
	switch input.Action {
	case "create", "status":
	case "step":
		if _, err = task.task.Step(req.Context(), p); err != nil {
			reject(503, routerredact.SafeFailureCode(err.Error()))
			return
		}
	case "cancel":
		task.task.Cancel()
		r.historyMu.Lock()
		delete(r.historyTasks, input.ID)
		r.historyMu.Unlock()
	default:
		reject(400, "invalid_request")
		return
	}
	providerJSON(w, 200, map[string]any{"schemaVersion": 1, "id": input.ID, "progress": task.task.Progress(), "cloudAttempts": 0})
}
