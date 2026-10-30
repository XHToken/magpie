package routerservice

import (
	"bytes"
	"encoding/json"

	"github.com/yetone/magpie/internal/routercloud"
)

const redactionTaskInstruction = "Redaction tokens of the form <TYPE:namespace:number> represent values that have already been supplied. Treat them as valid opaque data, not blank or missing fields. Complete the user's task using these tokens and preserve each token verbatim in text and tool arguments so the local application can restore the values. Do not invent their originals, label them unfilled, ask the user to resend the original sensitive values, or claim the user did not provide them. Do not volunteer explanations of redaction. If a specific operation requires unavailable properties of a hidden value, explain that specific limitation and complete the remaining work."

// This contract is added after recognition and target preparation. It contains
// no originals or issued mapping tokens and never enters the privacy model.
func addRedactionTaskInstruction(protocol string, body []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var root map[string]any
	if decoder.Decode(&root) != nil || root == nil {
		return nil, ErrRedactionUnavailable
	}
	switch protocol {
	case "chat":
		messages, ok := root["messages"].([]any)
		if !ok {
			return nil, ErrRedactionUnavailable
		}
		at := 0
		for at < len(messages) {
			message, _ := messages[at].(map[string]any)
			if message["role"] != "system" && message["role"] != "developer" {
				break
			}
			at++
		}
		withInstruction := make([]any, 0, len(messages)+1)
		withInstruction = append(withInstruction, messages[:at]...)
		withInstruction = append(withInstruction, map[string]any{"role": "system", "content": redactionTaskInstruction})
		root["messages"] = append(withInstruction, messages[at:]...)
	case "responses":
		if value, ok := root["instructions"].(string); ok && value != "" {
			root["instructions"] = value + "\n\n" + redactionTaskInstruction
		} else {
			root["instructions"] = redactionTaskInstruction
		}
	case "anthropic":
		switch value := root["system"].(type) {
		case nil:
			root["system"] = redactionTaskInstruction
		case string:
			root["system"] = value + "\n\n" + redactionTaskInstruction
		case []any:
			root["system"] = append(value, map[string]any{"type": "text", "text": redactionTaskInstruction})
		default:
			return nil, ErrRedactionUnavailable
		}
	default:
		return nil, ErrRedactionUnavailable
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(root) != nil || routercloud.ValidateInput(protocol, out.Bytes()) != nil {
		return nil, ErrRedactionUnavailable
	}
	return out.Bytes(), nil
}
