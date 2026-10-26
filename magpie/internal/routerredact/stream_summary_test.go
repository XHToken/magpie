package routerredact

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/cloudcodec"
)

func TestSSEResponsesWholeReplyKeepsSeparateReasoningSummaries(t *testing.T) {
	s := &Snapshot{mapping: map[string]string{"<PERSON:1>": "合成人物"}}
	defer s.Close()
	body := []byte(`{"id":"reply","object":"response","status":"completed","output":[{"id":"reasoning","type":"reasoning","summary":[{"type":"summary_text","text":"first <PERSON:1>"},{"type":"summary_text","text":"second <PERSON:1>"}]}]}`)
	events := cloudcodec.ResponsesWholeEvents(body)
	if len(events) == 0 {
		t.Fatal("whole reply did not produce stream events")
	}
	var input strings.Builder
	for _, event := range events {
		input.WriteString(streamTestEvent(event.Data))
	}
	var output bytes.Buffer
	if err := s.RestoreSSE(context.Background(), Responses, strings.NewReader(input.String()), &output); err != nil {
		t.Fatalf("valid multi-summary response failed restoration: %v", err)
	}
	_, _, restored := streamTestCollect(t, Responses, output.String())
	summaries := map[string]string{}
	for _, event := range restored {
		if event["type"] == "response.reasoning_summary_text.delta" {
			summaries[streamIndex(event["summary_index"])] += streamString(event["delta"])
		}
	}
	want := map[string]string{"0": "first 合成人物", "1": "second 合成人物"}
	if !reflect.DeepEqual(summaries, want) {
		t.Fatalf("reasoning summary deltas = %#v, want %#v", summaries, want)
	}
}

func TestSSEResponsesReasoningSummaryFlushKeepsIndex(t *testing.T) {
	s := &Snapshot{mapping: map[string]string{"<PERSON:1>": "合成人物"}}
	defer s.Close()
	event := func(typ string, index int, key, text string) string {
		return streamTestEvent(map[string]any{
			"type": typ, "item_id": "reasoning", "output_index": 0,
			"summary_index": index, "sequence_number": 1, key: text,
		})
	}
	input := event("response.reasoning_summary_text.delta", 1, "delta", "<PERSON:") +
		event("response.reasoning_summary_text.delta", 10, "delta", "other") +
		event("response.reasoning_summary_text.done", 10, "text", "other") +
		event("response.reasoning_summary_text.delta", 1, "delta", "1> <PERSON:") +
		event("response.reasoning_summary_text.done", 1, "text", "<PERSON:1> <PERSON:") +
		streamTestEnd(Responses, false)
	var output bytes.Buffer
	if err := s.RestoreSSE(context.Background(), Responses, strings.NewReader(input), &output); err != nil {
		t.Fatalf("interleaved summary restoration failed: %v", err)
	}
	_, _, restored := streamTestCollect(t, Responses, output.String())
	summaries := map[string]string{}
	for _, event := range restored {
		if event["type"] == "response.reasoning_summary_text.delta" {
			summaries[streamIndex(event["summary_index"])] += streamString(event["delta"])
		}
	}
	want := map[string]string{"1": "合成人物 <PERSON:", "10": "other"}
	if !reflect.DeepEqual(summaries, want) {
		t.Fatalf("reasoning summary channels/flush = %#v, want %#v", summaries, want)
	}
}
