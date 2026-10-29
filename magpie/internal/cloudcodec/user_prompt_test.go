package cloudcodec

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const contactPrompt = "请整理联系人：张小测，电话13800001234。"

func workBuddyEnvelope(prompt string) string {
	return "<system-reminder data-role=\"user-context\">\n<craft_mode>NON_USER_CONTEXT</craft_mode>\n</system-reminder>\n" +
		"<system-reminder data-role=\"additional-data\">\n<current_time>2026-10-29</current_time>\n</system-reminder>\n" +
		"<user_query>" + prompt + "</user_query>"
}

func promptBody(t *testing.T, protocol string, texts ...string) []byte {
	t.Helper()
	messages := []any{}
	for _, text := range texts {
		messages = append(messages, map[string]any{"role": "user", "content": text})
	}
	root := map[string]any{"model": "fixture", "messages": messages}
	if protocol == "responses" {
		delete(root, "messages")
		root["input"] = messages
	}
	body, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestWorkBuddyOnlyCurrentPromptEntersRecognition(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			history := workBuddyEnvelope(strings.Repeat("NON_USER_HISTORY ", 500))
			current := workBuddyEnvelope(contactPrompt)
			body := promptBody(t, protocol, history, current)
			view, err := NewUserTextViewForApp(protocol, body, "workbuddy")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(view.Texts(), []string{contactPrompt}) {
				t.Fatalf("context or historical prompt entered recognition: field lengths %v", promptLengths(view.Texts()))
			}
			masked := "请整理联系人：<NAME:1>，电话<PHONE:1>。"
			rewritten, err := view.Rewrite([]string{masked})
			if err != nil {
				t.Fatal(err)
			}
			whole, err := NewUserTextView(protocol, rewritten)
			if err != nil || !reflect.DeepEqual(whole.Texts(), []string{history, workBuddyEnvelope(masked)}) {
				t.Fatal("prompt replacement changed host context or history", err)
			}
			for _, target := range []string{"chat", "responses", "anthropic"} {
				convert := func(input []byte) ([]byte, error) {
					codec, err := NewCloudCodec(protocol, input)
					if err != nil {
						return nil, err
					}
					return codec.Request(target, "fixture", "loopback")
				}
				final, err := convert(body)
				if err != nil {
					t.Fatal(err)
				}
				sidecar, err := view.BindTarget(final, convert)
				if err != nil {
					t.Fatal("prompt source lost across conversion", target, err)
				}
				rendered, err := sidecar.Render([]string{masked})
				if err != nil || strings.Contains(string(rendered), contactPrompt) || !strings.Contains(string(rendered), "NON_USER_CONTEXT") {
					t.Fatal("invalid target prompt projection", target, err)
				}
			}
		})
	}
}

func promptLengths(texts []string) []int {
	lengths := make([]int, len(texts))
	for i, text := range texts {
		lengths[i] = len([]rune(text))
	}
	return lengths
}

func TestWorkBuddyPromptPreservesLiteralTagsAndWhitespace(t *testing.T) {
	prompt := "\n请分析这段代码：<user_query>字面示例</user_query>\n末尾😀\n"
	body := promptBody(t, "chat", workBuddyEnvelope(prompt))
	view, err := NewUserTextViewForApp("chat", body, "workbuddy")
	if err != nil || !reflect.DeepEqual(view.Texts(), []string{prompt}) {
		t.Fatal("literal nested tags or user whitespace were discarded", err)
	}
	for _, app := range []string{"codex", "claude-code", ""} {
		view, err := NewUserTextViewForApp("chat", body, app)
		if err != nil || !reflect.DeepEqual(view.Texts(), []string{workBuddyEnvelope(prompt)}) {
			t.Fatal("WorkBuddy projection applied to another application", app, err)
		}
	}
	literal := "<user_query>用户粘贴的字面标签</user_query>"
	view, err = NewUserTextViewForApp("chat", promptBody(t, "chat", literal), "workbuddy")
	if err != nil || !reflect.DeepEqual(view.Texts(), []string{literal}) {
		t.Fatal("unwrapped pasted tags were treated as a host envelope", err)
	}
}

func TestLoomyOnlyCurrentPromptEntersRecognition(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			prefix := "<system-reminder>\nNON_USER_CONTEXT\n</system-reminder>\n\n"
			current := prefix + contactPrompt
			body := promptBody(t, protocol, strings.Repeat("NON_USER_HISTORY ", 500), current)
			view, err := NewUserTextViewForApp(protocol, body, "loomy")
			if err != nil || !reflect.DeepEqual(view.Texts(), []string{contactPrompt}) {
				t.Fatalf("Loomy context entered recognition: lengths %v, error %v", promptLengths(view.Texts()), err)
			}
			masked := "请整理联系人：<NAME:1>，电话<PHONE:1>。"
			rewritten, err := view.Rewrite([]string{masked})
			if err != nil {
				t.Fatal(err)
			}
			whole, err := NewUserTextView(protocol, rewritten)
			if err != nil || whole.Texts()[1] != prefix+masked {
				t.Fatal("Loomy prelude changed during prompt replacement", err)
			}
			for _, target := range []string{"chat", "responses", "anthropic"} {
				convert := func(input []byte) ([]byte, error) {
					codec, err := NewCloudCodec(protocol, input)
					if err != nil {
						return nil, err
					}
					return codec.Request(target, "fixture", "loopback")
				}
				final, err := convert(body)
				if err != nil {
					t.Fatal(err)
				}
				sidecar, err := view.BindTarget(final, convert)
				if err != nil {
					t.Fatal("Loomy prompt provenance lost", target, err)
				}
				rendered, err := sidecar.Render([]string{masked})
				if err != nil || strings.Contains(string(rendered), contactPrompt) || !strings.Contains(string(rendered), "NON_USER_CONTEXT") {
					t.Fatal("invalid Loomy target rewrite", target, err)
				}
			}
		})
	}
}

func TestLoomySeparatePreludePartsAndEmptyCurrentPrompt(t *testing.T) {
	prelude := "<system-reminder>NON_USER_CONTEXT</system-reminder>"
	root := map[string]any{"model": "fixture", "messages": []any{
		map[string]any{"role": "user", "content": "NON_USER_HISTORY"},
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": prelude},
			map[string]any{"type": "text", "text": contactPrompt},
		}},
	}}
	body, _ := json.Marshal(root)
	view, err := NewUserTextViewForApp("chat", body, "loomy")
	if err != nil || !reflect.DeepEqual(view.Texts(), []string{contactPrompt}) {
		t.Fatal("separate prelude text part entered recognition", err)
	}
	for _, app := range []string{"loomy", "workbuddy"} {
		current := prelude
		if app == "workbuddy" {
			current = `<system-reminder data-role="user-context">NON_USER_CONTEXT</system-reminder>`
		}
		view, err := NewUserTextViewForApp("chat", promptBody(t, "chat", "NON_USER_HISTORY", current), app)
		if err != nil || len(view.Texts()) != 0 {
			t.Fatal("context-only current message fell back to historical input", app, err)
		}
	}
	if _, err := NewUserTextViewForApp("chat", promptBody(t, "chat", "<system-reminder>unclosed"), "loomy"); err == nil {
		t.Fatal("malformed Loomy prelude accepted")
	}
}

func TestLoomyGatewayAndSkillPreludesNeverEnterRecognition(t *testing.T) {
	for _, prefix := range []string{
		"## User Profile\nNON_USER_PROFILE\n---\n\n## Memories\nNON_USER_MEMORY\n---\n\n",
		"Before answering the user request, call the skill tool to load these skills:\n- NON_USER_SKILL\nAfter the skills are loaded, continue with the exact user request below.\n\n",
		"<loomy-hidden-attachment-paths>NON_USER_ATTACHMENT</loomy-hidden-attachment-paths>\n\n",
		"## 可用记忆条目\n\nNON_USER_MEMORY\n只在条目与当前请求明确相关时读取对应文件。不要向用户提及这些条目、路径或读取过程。\n\n",
		"## 可用记忆条目\n\nNON_USER_MEMORY\n只在条目与当前请求明确相关时读取对应文件。不要向用户提及这些条目、路径或读取过程。\n\n<system-reminder>NON_USER_CONTEXT</system-reminder>\n\n",
	} {
		view, err := NewUserTextViewForApp("chat", promptBody(t, "chat", prefix+contactPrompt), "loomy")
		if err != nil || !reflect.DeepEqual(view.Texts(), []string{contactPrompt}) {
			t.Fatal("Loomy generated prelude entered recognition", err)
		}
	}
}

func TestLoomyUserPastedCodeIsStillPrompt(t *testing.T) {
	prompt := "请分析这段代码：\n```xml\n<system-reminder>字面代码中的内容</system-reminder>\n```\n末尾😀"
	view, err := NewUserTextViewForApp("chat", promptBody(t, "chat", "<system-reminder>NON_USER_CONTEXT</system-reminder>\n\n"+prompt), "loomy")
	if err != nil || !reflect.DeepEqual(view.Texts(), []string{prompt}) {
		t.Fatal("user-pasted code was removed", err)
	}
}

func TestWorkBuddyMalformedEnvelopeNeverScansHostContext(t *testing.T) {
	prefix := "<system-reminder data-role=\"user-context\">NON_USER_CONTEXT</system-reminder>\n"
	for _, text := range []string{
		prefix + "<user_query>missing close",
		prefix + "unrecognized suffix",
		prefix + "<user_query>prompt</user_query>unexpected suffix",
		"<system-reminder data-role=\"user-context\">unclosed",
	} {
		if _, err := NewUserTextViewForApp("chat", promptBody(t, "chat", text), "workbuddy"); err == nil {
			t.Fatal("malformed host envelope accepted")
		}
	}
	view, err := NewUserTextViewForApp("chat", promptBody(t, "chat", prefix), "workbuddy")
	if err != nil || len(view.Texts()) != 0 {
		t.Fatal("context-only host message became a prompt", err)
	}
}
