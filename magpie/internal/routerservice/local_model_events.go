package routerservice

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/yetone/magpie/internal/routerruntime"
)

// localModelEvents waits on Manager notifications, not a second polling loop.
// Slow readers receive the latest snapshot; download callbacks never wait on UI.
func localModelEvents(w http.ResponseWriter, r *http.Request, manager *routerruntime.Manager) {
	w.Header().Set("Content-Type", "text/event-stream")
	if r.Method == http.MethodHead {
		return
	}
	controller := http.NewResponseController(w)
	defer controller.SetWriteDeadline(time.Time{})
	write := func(payload []byte) bool {
		_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := w.Write(payload); err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		status, changed := manager.WatchStatus()
		payload, err := json.Marshal(struct {
			SchemaVersion int                  `json:"schemaVersion"`
			Status        routerruntime.Status `json:"status"`
		}{1, status})
		if err != nil || !write(append(append([]byte("data: "), payload...), '\n', '\n')) || changed == nil {
			return
		}
		// Coalesce bursts to at most two snapshots per second, including when
		// a client is slow. The captured signal also covers changes during writes.
		for {
			select {
			case <-r.Context().Done():
				return
			case <-changed:
				delay := time.NewTimer(500 * time.Millisecond)
				select {
				case <-r.Context().Done():
					delay.Stop()
					return
				case <-delay.C:
				}
			case <-heartbeat.C:
				if !write([]byte(": keep-alive\n\n")) {
					return
				}
				continue
			}
			break
		}
	}
}
