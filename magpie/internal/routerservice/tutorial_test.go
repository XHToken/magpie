package routerservice

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTutorialDismissalSurvivesRestartAndPortChange(t *testing.T) {
	l, address := fixture(t)
	config, err := os.ReadFile(l.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport}
	request := func(method, path, address string, authorized bool) (int, map[string]any) {
		t.Helper()
		r, err := http.NewRequest(method, "http://"+address+path, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		if authorized {
			token, err := os.ReadFile(TokenFile(l))
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		var data map[string]any
		if response.StatusCode == http.StatusOK && json.Unmarshal(body, &data) != nil {
			t.Fatalf("invalid response: %s", body)
		}
		return response.StatusCode, data
	}
	_, cancel, done := start(t, l)
	if status, data := request("GET", "/api/service", address, true); status != 200 || data["tutorialDismissed"] != false {
		t.Fatalf("first launch must report an unseen tutorial: %d %v", status, data)
	}
	if status, _ := request("POST", "/api/tutorial/dismiss", address, false); status != 401 {
		t.Fatalf("tutorial preference must require management auth: %d", status)
	}
	if status, _ := request("POST", "/api/tutorial/dismiss", address, true); status != 200 {
		t.Fatalf("dismiss tutorial: %d", status)
	}
	if status, _ := request("POST", "/api/tutorial/dismiss", address, true); status != 200 {
		t.Fatalf("repeated dismissal must be idempotent: %d", status)
	}
	if after, err := os.ReadFile(l.ConfigFile()); err != nil || string(after) != string(config) {
		t.Fatalf("tutorial preference changed service configuration: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Keep the original port occupied so the next managed startup must move.
	occupied, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	info, _, _ := start(t, l)
	if info.Address == address {
		t.Fatal("restart must use a new management port")
	}
	if status, data := request("GET", "/api/service", info.Address, true); status != 200 || data["tutorialDismissed"] != true {
		t.Fatalf("dismissal must survive restart and port change: %d %v", status, data)
	}
	// Unreadable/corrupt state is unknown, never silently reset or overwritten.
	stateFile := filepath.Join(l.State, "tutorial-dismissed")
	if err := os.WriteFile(stateFile, []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if status, data := request("GET", "/api/service", info.Address, true); status != 200 || data["tutorialDismissed"] != nil {
		t.Fatalf("corrupt tutorial state must be unknown: %d %v", status, data)
	}
	if status, _ := request("POST", "/api/tutorial/dismiss", info.Address, true); status != 503 {
		t.Fatalf("corrupt tutorial state must remain intact: %d", status)
	}
	if data, err := os.ReadFile(stateFile); err != nil || string(data) != "damaged" {
		t.Fatalf("corrupt state was overwritten: %q %v", data, err)
	}
	if err := os.Remove(stateFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateFile, 0o700); err != nil {
		t.Fatal(err)
	}
	if status, data := request("GET", "/api/service", info.Address, true); status != 200 || data["tutorialDismissed"] != nil {
		t.Fatalf("unreadable tutorial state must be unknown: %d %v", status, data)
	}
}
