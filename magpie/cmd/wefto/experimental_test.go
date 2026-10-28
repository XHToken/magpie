package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/testenv"
)

func TestExperimentalServeIsExplicitAndCancelable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if err := runContext(ctx, []string{"serve", "--experimental-redaction"}, &out); !errors.Is(err, context.Canceled) {
		t.Fatal("explicit trial serve was not admitted to canceled service startup", err)
	}
	for _, args := range [][]string{{"serve", "--experimental-redaction=true"}, {"serve", "--experimental-redaction", "--addr", "0.0.0.0:13145"}, {"healthcheck", "--experimental-redaction"}} {
		if err := runContext(ctx, args, &out); err == nil || !strings.Contains(err.Error(), "disabled in wefto") {
			t.Fatal("unrecognized trial/serve options were accepted", args, err)
		}
	}
}

type cancelReadyWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelReadyWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if strings.Contains(string(p), "Management:") {
		w.cancel()
	}
	return n, err
}

func TestServeRedactionDefaultAndExplicitOptOut(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{"default", []string{"serve"}, true},
		{"explicit-on", []string{"serve", "--experimental-redaction"}, true},
		{"explicit-off", []string{"serve", "--no-experimental-redaction"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			testenv.SetHome(t, home)
			for _, key := range []string{"APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
				t.Setenv(key, filepath.Join(home, key))
			}
			layout, err := dirs.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			out := &cancelReadyWriter{cancel: cancel}
			if err := runContext(ctx, tc.args, out); err != nil {
				t.Fatal("serve did not shut down after readiness", err)
			}
			var saved struct {
				ExperimentalRedaction bool `json:"experimentalRedaction"`
			}
			data, err := os.ReadFile(filepath.Join(layout.State, "service-options.json"))
			if err != nil || json.Unmarshal(data, &saved) != nil || saved.ExperimentalRedaction != tc.want {
				t.Fatalf("saved startup redaction = %t, want %t; read error: %v", saved.ExperimentalRedaction, tc.want, err)
			}
			if strings.Contains(out.String(), "Experimental redaction trial: enabled") != tc.want {
				t.Fatal("startup notice disagrees with redaction choice")
			}
		})
	}
}
