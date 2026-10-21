package routerservice

import (
	"net/http"
	"net/http/httptest"
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
