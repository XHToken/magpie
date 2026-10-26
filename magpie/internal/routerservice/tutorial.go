package routerservice

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"time"

	"github.com/yetone/magpie/internal/steady"
)

// This service-owned marker is independent of browser origins and model config.
// It is monotonic: reading again never resets the first-display preference.
const tutorialDismissedRecord = "1\n"

func tutorialDismissed(ctx context.Context, file string) (bool, error) {
	if file == "" {
		return false, errors.New("tutorial state unavailable")
	}
	data, err := steady.ReadFileContext(ctx, file)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if string(data) != tutorialDismissedRecord {
		return false, errors.New("invalid tutorial state")
	}
	return true, nil
}

func tutorialDismissAPI(w http.ResponseWriter, r *http.Request, file string) {
	if r.URL.RawQuery != "" {
		providerJSON(w, 400, map[string]string{"error": "invalid query"})
		return
	}
	var input struct{}
	if !providerInput(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	dismissed, err := tutorialDismissed(ctx, file)
	if err == nil && !dismissed {
		err = steady.WriteFileContext(ctx, file, []byte(tutorialDismissedRecord), 0o600)
	}
	if err != nil {
		providerJSON(w, 503, map[string]string{"error": "tutorial state unavailable"})
		return
	}
	providerJSON(w, 200, map[string]bool{"dismissed": true})
}
