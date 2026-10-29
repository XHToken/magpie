package cloudcodec

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestUserInputSourceProjectionAndNineConversions(t *testing.T) {
	for _, source := range cloudProtocols {
		for _, target := range cloudProtocols {
			t.Run(string(source)+"-"+string(target), func(t *testing.T) {
				body := cloudRequestFixture(t, source, false)
				view, err := NewUserTextView(string(source), body)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(view.Texts(), []string{"use the tool", "continue"}) {
					t.Fatal("tool results became user input", view.Texts())
				}
				convert := func(input []byte) ([]byte, error) {
					c, e := NewCloudCodec(string(source), input)
					if e != nil {
						return nil, e
					}
					return c.Request(string(target), "model", "loopback")
				}
				final, err := convert(body)
				if err != nil {
					t.Fatal(err)
				}
				sidecar, err := view.BindTarget(final, convert)
				if err != nil {
					t.Fatal("lost provenance", err)
				}
				rendered, err := sidecar.Render([]string{"<NAME:1>", "continue masked"})
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(rendered, []byte("use the tool")) || !bytes.Contains(rendered, []byte("one")) {
					t.Fatal("source replacement touched tool result", string(rendered))
				}
				if strings.Contains(string(rendered), "__router_user_") {
					t.Fatal("internal source ID escaped")
				}
				// User-origin text remains protected even when the last item is a tool.
				bad := func(input []byte) ([]byte, error) {
					raw, e := convert(input)
					if e != nil {
						return nil, e
					}
					var o map[string]any
					json.Unmarshal(raw, &o)
					if target == Responses {
						o["input"] = []any{}
					} else {
						o["messages"] = []any{}
					}
					return json.Marshal(o)
				}
				conflict, _ := bad(body)
				if _, e := view.BindTarget(conflict, bad); e == nil {
					t.Fatal("overwritten sources accepted")
				}
			})
		}
	}
}
func TestUserProjectionExcludesProtocolLookalikes(t *testing.T) {
	tests := []struct {
		p, body string
		want    []string
	}{
		{"chat", `{"messages":[{"role":"system","content":"same"},{"role":"user","content":"same"},{"role":"tool","content":"tool"}]}`, []string{"same"}},
		{"anthropic", `{"messages":[{"role":"user","content":[{"type":"text","text":"user"},{"type":"tool_result","content":[{"type":"text","text":"tool"}]}]}]}`, []string{"user"}},
		{"responses", `{"instructions":"system","input":[{"type":"function_call_output","output":"tool"},{"role":"user","content":[{"type":"input_text","text":"user"}]}]}`, []string{"user"}},
	}
	for _, test := range tests {
		v, e := NewUserTextView(test.p, []byte(test.body))
		if e != nil || !reflect.DeepEqual(v.Texts(), test.want) {
			t.Fatal(test.p, e, v.Texts())
		}
	}
}
