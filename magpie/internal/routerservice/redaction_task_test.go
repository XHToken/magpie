package routerservice

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRedactionTaskInstructionPreservesProtocolData(t *testing.T) {
	for _, tc := range []struct{ name, protocol, body string }{
		{"chat", "chat", `{"model":"fixture","seed":9007199254740993,"messages":[{"role":"system","content":"EXISTING"},{"role":"developer","content":"DEVELOPER"},{"role":"user","content":"<NAME:1> <PHONE:1>"}]}`},
		{"responses", "responses", `{"model":"fixture","instructions":"EXISTING","input":"<NAME:1> <PHONE:1>"}`},
		{"responses-absent", "responses", `{"model":"fixture","input":"<NAME:1> <PHONE:1>"}`},
		{"anthropic", "anthropic", `{"model":"fixture","max_tokens":64,"system":"EXISTING","messages":[{"role":"user","content":"<NAME:1> <PHONE:1>"}]}`},
		{"anthropic-blocks", "anthropic", `{"model":"fixture","max_tokens":64,"system":[{"type":"text","text":"EXISTING","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"<NAME:1> <PHONE:1>"}]}`},
		{"anthropic-absent", "anthropic", `{"model":"fixture","max_tokens":64,"messages":[{"role":"user","content":"<NAME:1> <PHONE:1>"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := addRedactionTaskInstruction(tc.protocol, []byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			var before, after map[string]any
			for _, pair := range []struct {
				body   []byte
				target *map[string]any
			}{{[]byte(tc.body), &before}, {out, &after}} {
				d := json.NewDecoder(bytes.NewReader(pair.body))
				d.UseNumber()
				if err := d.Decode(pair.target); err != nil {
					t.Fatal(err)
				}
			}
			if strings.Count(string(out), redactionTaskInstruction) != 1 {
				t.Fatal("instruction not attached exactly once")
			}
			switch tc.protocol {
			case "chat":
				messages := after["messages"].([]any)
				if messages[2].(map[string]any)["content"] != redactionTaskInstruction {
					t.Fatal("instruction lost system priority")
				}
				after["messages"] = append(messages[:2], messages[3:]...)
			case "responses":
				if before["instructions"] == nil {
					delete(after, "instructions")
				} else {
					after["instructions"] = strings.TrimSuffix(after["instructions"].(string), "\n\n"+redactionTaskInstruction)
				}
			case "anthropic":
				switch before["system"].(type) {
				case nil:
					delete(after, "system")
				case string:
					after["system"] = strings.TrimSuffix(after["system"].(string), "\n\n"+redactionTaskInstruction)
				default:
					blocks := after["system"].([]any)
					after["system"] = blocks[:len(blocks)-1]
				}
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("instruction changed task, protocol fields, existing policy or number precision")
			}
		})
	}
}
