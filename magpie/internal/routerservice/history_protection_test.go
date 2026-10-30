package routerservice

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/yetone/magpie/internal/routercloud"
	"github.com/yetone/magpie/internal/routerconfig"
	"strings"
	"testing"
)

func TestHostHistoryKnownValueProtection(t *testing.T) {
	for _, app := range []string{"workbuddy", "loomy"} {
		t.Run(app, func(t *testing.T) {
			v := &currentPromptInvoker{t: t}
			opts := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: v}})
			if opts.Redaction.resultsError != nil {
				t.Fatal("store open", opts.Redaction.resultsError)
			}
			for turn := 0; turn < 2; turn++ {
				messages := []any{map[string]any{"role": "user", "content": "电话13800001234"}}
				if turn == 1 {
					messages = append(messages, map[string]any{"role": "assistant", "content": "电话13800001234"}, map[string]any{"role": "user", "content": "继续"})
				}
				body, _ := json.Marshal(map[string]any{"model": "fixture", "messages": messages})
				p, err := opts.Redaction.Prepare(context.Background(), RedactionRequest{RequestID: "history-regression", AppID: app, SourceProtocol: "chat", TargetProtocol: "chat", Operation: "generation", Body: body})
				if err != nil {
					t.Fatalf("%#v", err)
				}
				leaked := bytes.Contains(p.Body, []byte("13800001234"))
				closePreparedCloud(p)
				if leaked {
					t.Fatalf("turn %d leaked the historical phone in final cloud body", turn+1)
				}
			}
			if len(v.texts) != 2 || len(v.texts[1]) != 1 || v.texts[1][0] != "继续" {
				t.Fatalf("history re-entered recognition: %v", v.texts)
			}
		})
	}
}

func TestHistoryAllProtocolPairsPreserveProtectionAndRestoration(t *testing.T) {
	for _, source := range []string{"chat", "responses", "anthropic"} {
		for _, target := range []string{"chat", "responses", "anthropic"} {
			t.Run(source+"/"+target, func(t *testing.T) {
				v := &currentPromptInvoker{t: t}
				opts := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: v}})
				for turn := 0; turn < 2; turn++ {
					messages := []any{map[string]any{"role": "user", "content": "电话13800001234"}}
					if turn == 1 {
						messages = append(messages, map[string]any{"role": "assistant", "content": "旧回显13800001234"}, map[string]any{"role": "user", "content": "继续"})
					}
					root := map[string]any{"model": "fixture", "messages": messages}
					if source == "responses" {
						delete(root, "messages")
						root["input"] = messages
					}
					if source == "anthropic" {
						root["max_tokens"] = 128
					}
					body, _ := json.Marshal(root)
					wire, e := routercloud.PrepareRequestBody(routercloud.Request{Protocol: source, Body: body, Target: routercloud.Target{Model: "fixture", Provider: routerconfig.ResolvedProvider{Config: routerconfig.CloudProvider{Protocol: target, BaseURL: "http://127.0.0.1/v1"}}}}, false)
					if e != nil {
						t.Fatal(e)
					}
					sidecar, e := wire.UserInputTarget("workbuddy")
					if e != nil {
						t.Fatal(e)
					}
					p, e := opts.Redaction.Prepare(context.Background(), RedactionRequest{RequestID: "protocol-history", AppID: "workbuddy", SourceProtocol: source, TargetProtocol: target, Operation: "generation", SourceBody: body, Body: wire.Body(), UserTarget: sidecar})
					if e != nil {
						t.Fatal(e)
					}
					if bytes.Contains(p.Body, []byte("13800001234")) {
						t.Fatal("historical value escaped target conversion")
					}
					if len(p.ObservationMeta.Replacements) != 1 {
						t.Fatal("missing restoration authority")
					}
					token := p.ObservationMeta.Replacements[0].Placeholder
					var response []byte
					switch source {
					case "chat":
						response = []byte(`{"choices":[{"message":{"role":"assistant","content":"` + token + `"},"finish_reason":"stop"}]}`)
					case "responses":
						response = []byte(`{"object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + token + `"}]}]}`)
					case "anthropic":
						response = []byte(`{"type":"message","role":"assistant","content":[{"type":"text","text":"` + token + `"}],"stop_reason":"end_turn"}`)
					}
					restored, e := p.Restore(context.Background(), source, "application/json", response)
					closePreparedCloud(p)
					if e != nil || !strings.Contains(string(restored), "13800001234") {
						t.Fatal("client response restoration failed", e)
					}
				}
				if len(v.texts) != 2 || len(v.texts[1]) != 1 || v.texts[1][0] != "继续" {
					t.Fatal("history was rescanned", v.texts)
				}
			})
		}
	}
}
