package cloudcodec

import (
	"fmt"
	"strings"
	"unicode"
)

// UserPromptSourceVersion identifies the application-specific prompt projection.
const UserPromptSourceVersion = "workbuddy-loomy-current-prompt-v2"

// NewUserTextViewForApp applies the authenticated application's prompt contract.
func NewUserTextViewForApp(protocol string, body []byte, appID string) (*UserTextView, error) {
	return newUserPromptView(protocol, body, appID, false)
}

// NewUserHistoryViewForApp projects every actual user message. The current-only
// view remains available for prompt previews and title generation.
func NewUserHistoryViewForApp(protocol string, body []byte, appID string) (*UserTextView, error) {
	return newUserPromptView(protocol, body, appID, true)
}

func newUserPromptView(protocol string, body []byte, appID string, history bool) (*UserTextView, error) {
	v, err := NewUserTextView(protocol, body)
	if v != nil {
		v.history = history
	}
	if err != nil || (appID != "workbuddy" && appID != "loomy") {
		return v, err
	}
	v.appID = appID
	slots := v.slots
	v.slots, v.texts, v.identities = nil, nil, nil
	if len(slots) == 0 {
		return v, nil
	}
	latest := v.latestUser
	for _, slot := range slots {
		if !history && slot.user != latest {
			continue
		}
		text := slot.object[slot.key].(string)
		var spans [][2]int
		if appID == "workbuddy" {
			spans, err = workBuddyPromptSpans(text)
		} else {
			spans, err = loomyPromptSpans(text)
		}
		if err != nil {
			return nil, err
		}
		for _, span := range spans {
			s := slot
			s.start, s.end = span[0], span[1]
			v.slots = append(v.slots, s)
			v.texts = append(v.texts, text[s.start:s.end])
			v.identities = append(v.identities, fmt.Sprintf("%s/%s/current/%d", UserPromptSourceVersion, appID, len(v.slots)-1))
		}
	}
	return v, nil
}

// UserMessage groups ordered exact prompt blocks without absolute positions in
// its content identity. Indices refer only to this view, never client IDs.
type UserMessage struct {
	Ordinal int
	Fields  []int
	Texts   []string
	Current bool
}

func (v *UserTextView) Messages() []UserMessage {
	var messages []UserMessage
	for i, slot := range v.slots {
		if len(messages) == 0 || messages[len(messages)-1].Ordinal != slot.user {
			messages = append(messages, UserMessage{Ordinal: slot.user, Current: slot.user == v.latestUser && v.endsWithUser()})
		}
		m := &messages[len(messages)-1]
		m.Fields = append(m.Fields, i)
		m.Texts = append(m.Texts, v.texts[i])
	}
	return messages
}

func (v *UserTextView) endsWithUser() bool {
	if _, ok := v.root["input"].(string); ok {
		return true
	}
	key := "messages"
	if v.protocol == "responses" {
		key = "input"
	}
	items, _ := v.root[key].([]any)
	if len(items) == 0 {
		return false
	}
	o, _ := items[len(items)-1].(map[string]any)
	if o["role"] != "user" {
		return false
	}
	if blocks, ok := o["content"].([]any); ok {
		for _, raw := range blocks {
			b, _ := raw.(map[string]any)
			if b["type"] == "tool_result" {
				return false
			}
		}
	}
	return true
}

// WorkBuddy's installed serializer emits attributed reminders followed by one
// outer user_query. The last closing tag belongs to that outer envelope; tags
// pasted inside the actual prompt remain literal input, including whitespace.
func workBuddyPromptSpans(text string) ([][2]int, error) {
	start := len(text) - len(strings.TrimLeftFunc(text, unicode.IsSpace))
	if !strings.HasPrefix(text[start:], `<system-reminder data-role="user-context">`) &&
		!strings.HasPrefix(text[start:], `<system-reminder data-role="additional-data">`) {
		return [][2]int{{0, len(text)}}, nil
	}
	for {
		remaining := text[start:]
		if !strings.HasPrefix(remaining, `<system-reminder data-role="user-context">`) &&
			!strings.HasPrefix(remaining, `<system-reminder data-role="additional-data">`) {
			break
		}
		end, err := reminderEnd(text, start, "system-reminder")
		if err != nil {
			return nil, err
		}
		start = end + len(text[end:]) - len(strings.TrimLeftFunc(text[end:], unicode.IsSpace))
	}
	if start == len(text) {
		return nil, nil
	}
	const open, close = "<user_query>", "</user_query>"
	trimmed := strings.TrimRightFunc(text, unicode.IsSpace)
	if !strings.HasPrefix(text[start:], open) || !strings.HasSuffix(trimmed, close) {
		return nil, ErrUserInputScope
	}
	return [][2]int{{start + len(open), len(trimmed) - len(close)}}, nil
}

// Loomy's prompt assembler prepends separate system-reminder text parts; its
// OpenCode transport may concatenate them with the user text. Retain only text
// outside the host's explicitly delimited reminders/attachment metadata.
func loomyPromptSpans(text string) ([][2]int, error) {
	var spans [][2]int
	start, err := loomyPreludeEnd(text, 0)
	if err != nil {
		return nil, err
	}
	at := start
	for at < len(text) {
		if (at == 0 || text[at-1] == '\n') && (strings.HasPrefix(text[at:], "```") || strings.HasPrefix(text[at:], "~~~")) {
			fence := text[at : at+3]
			end := strings.Index(text[at+3:], "\n"+fence)
			if end < 0 {
				break
			}
			at += 3 + end + 1 + len(fence)
			continue
		}
		tag := ""
		for _, name := range []string{"system-reminder", "loomy-channel-rules", "loomy-hidden-attachment-paths", "loomy-visible-attachment-paths", "loomy-hidden-forced-skills"} {
			if strings.HasPrefix(text[at:], "<"+name+">") || strings.HasPrefix(text[at:], "<"+name+" ") {
				tag = name
				break
			}
		}
		if tag == "" {
			at++
			continue
		}
		end, err := reminderEnd(text, at, tag)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(text[start:at]) != "" {
			spans = append(spans, [2]int{start, at})
		}
		at, start = end, end
	}
	if strings.TrimSpace(text[start:]) != "" {
		// Separators inserted between prelude parts and the prompt are not input.
		if len(spans) == 0 && start > 0 {
			start += len(text[start:]) - len(strings.TrimLeftFunc(text[start:], unicode.IsSpace))
		}
		if len(spans) == 0 {
			var err error
			start, err = loomyPreludeEnd(text, start)
			if err != nil {
				return nil, err
			}
		}
		if strings.TrimSpace(text[start:]) != "" {
			spans = append(spans, [2]int{start, len(text)})
		}
	}
	return spans, nil
}

// These literal framing strings are emitted by Loomy's prompt assembler and
// gateway; only anchored, complete preludes are excluded from recognition.
func loomyPreludeEnd(text string, start int) (int, error) {
	for {
		remaining := text[start:]
		matched := false
		if strings.HasPrefix(remaining, "## 可用记忆条目\n\n") {
			end := -1
			for _, marker := range []string{
				"只在条目与当前请求明确相关时读取对应文件。不要向用户提及这些条目、路径或读取过程。",
				"当前没有可用记忆条目。不要调用 Read 读取记忆文件，也不要猜测任何文件路径；直接基于本轮对话自然回复。",
			} {
				if at := strings.Index(remaining, marker); at >= 0 && (end < 0 || at+len(marker) < end) {
					end = at + len(marker)
				}
			}
			if end < 0 {
				return 0, ErrUserInputScope
			}
			start += end
			start += len(text[start:]) - len(strings.TrimLeftFunc(text[start:], unicode.IsSpace))
			continue
		}
		for _, heading := range []string{"## User Profile", "## Memories", "## Learned Skills", "## User Memory Recall", "## 模型身份约束"} {
			if strings.HasPrefix(remaining, heading+"\n") || strings.HasPrefix(remaining, heading+"\r\n") {
				end := strings.Index(remaining, "\n---\n")
				if end < 0 {
					return 0, ErrUserInputScope
				}
				start += end + len("\n---\n")
				start += len(text[start:]) - len(strings.TrimLeftFunc(text[start:], unicode.IsSpace))
				matched = true
				break
			}
		}
		if !matched {
			break
		}
	}
	const skillPrefix = "Before answering the user request, call the skill tool to load these skills:"
	const skillSuffix = "After the skills are loaded, continue with the exact user request below."
	if strings.HasPrefix(text[start:], skillPrefix+"\n") {
		end := strings.Index(text[start:], "\n"+skillSuffix+"\n")
		if end < 0 {
			return 0, ErrUserInputScope
		}
		start += end + len("\n"+skillSuffix+"\n")
		start += len(text[start:]) - len(strings.TrimLeftFunc(text[start:], unicode.IsSpace))
	}
	return start, nil
}

func reminderEnd(text string, start int, tag string) (int, error) {
	open, close := "<"+tag, "</"+tag+">"
	depth := 0
	for at := start; at < len(text); {
		next := strings.IndexByte(text[at:], '<')
		if next < 0 {
			break
		}
		at += next
		if strings.HasPrefix(text[at:], close) {
			depth--
			at += len(close)
			if depth == 0 {
				return at, nil
			}
		} else if strings.HasPrefix(text[at:], open+">") || strings.HasPrefix(text[at:], open+" ") {
			end := strings.IndexByte(text[at:], '>')
			if end < 0 {
				break
			}
			depth++
			at += end + 1
		} else {
			at++
		}
	}
	return 0, ErrUserInputScope
}
