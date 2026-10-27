package routerservice

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacyManagementHeaderKeepsSameOriginBoundary(t *testing.T) {
	const address = "127.0.0.1:13145"
	for _, variant := range []string{"legacy", "both", "foreign", "conflicting", "duplicate"} {
		t.Run(variant, func(t *testing.T) {
			r := browserManagementRequest(http.MethodPost, address, "/api/management/session")
			r.Header.Set("X-API-Router-UI", "1")
			switch variant {
			case "legacy":
				r.Header.Del("X-Wefto-UI")
			case "foreign":
				r.Header.Set("Origin", "http://attacker.invalid")
			case "conflicting":
				r.Header.Set("X-Wefto-UI", "wrong")
			case "duplicate":
				r.Header.Add("X-API-Router-UI", "1")
			}
			w := httptest.NewRecorder()
			handler(Info{Service: "wefto", Address: address}, "private-test-token").ServeHTTP(w, r)
			want := http.StatusForbidden
			if variant == "legacy" || variant == "both" {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Fatalf("%s header returned %d, want %d", variant, w.Code, want)
			}
		})
	}
}

func TestConsoleServesWeftoNameAndWIcon(t *testing.T) {
	const address = "127.0.0.1:13145"
	h := handler(Info{Service: "wefto", Address: address}, "branding-test-token")
	for _, check := range []struct {
		path string
		want []string
	}{
		{"/", []string{"<title>Wefto", `aria-hidden="true">W</span>`, "/icons/favicon.svg?v=wefto-w-"}},
		{"/icons/favicon.svg", []string{">W</text>"}},
	} {
		r := httptest.NewRequest("GET", "http://"+address+check.path, nil)
		r.RemoteAddr = "127.0.0.1:2345"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: HTTP %d", check.path, w.Code)
		}
		for _, want := range check.want {
			if !strings.Contains(w.Body.String(), want) {
				t.Errorf("%s: Wefto branding missing %q", check.path, want)
			}
		}
	}
}
