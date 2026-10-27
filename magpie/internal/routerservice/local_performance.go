package routerservice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/routerprocess"
	"github.com/yetone/magpie/internal/routerruntime"
)

func (l *LocalModels) checkPerformance(ctx context.Context) error {
	binding := l.Manager.PerformanceBinding()
	identity := binding.Identity
	var samples []routerruntime.PerformanceSample
	for i := 0; i < 2; i++ {
		err := l.Manager.Use(ctx, routerruntime.Execution, func(ctx context.Context, endpoint string) error {
			if l.Manager.PerformanceBinding() != binding {
				return routerruntime.ErrCapability
			}
			bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			started := time.Now()
			var usage []byte
			sample, err := sampleLocalPerformance(bounded, endpoint, identity.ModelID, func(raw []byte) { usage = append([]byte(nil), raw...) })
			if l.Observer != nil {
				ended := time.Now()
				settings := l.Manager.Settings()
				var wrapped []byte
				if len(usage) > 0 {
					wrapped = append(append([]byte(`{"usage":`), usage...), byte('}'))
				}
				l.Observer.ObserveLocalCall(routerruntime.LocalCallObservation{Settings: settings, SettingsFingerprint: settings.Fingerprint(), CallID: nextLocalCallID(), Check: true, Purpose: routerruntime.LocalCallPurpose(routerruntime.Execution), Mode: routerruntime.LocalCallDiagnostic, ModelID: identity.ModelID, ModelFingerprint: identity.ModelFingerprint, RuntimeID: identity.RuntimeID, RuntimeFingerprint: identity.RuntimeFingerprint, MaterialVersion: "local-performance-v1", PromptVersion: localPromptVersion(settings), Suite: "local-performance-v1:" + identity.ModelFingerprint, StartedAt: started.UTC(), EndedAt: ended.UTC(), Outcome: localOutcome(err), ErrorCode: localErrorCode(err), Attempted: true, Usage: reportedUsageFromBody(wrapped), TotalMS: phase(float64(ended.Sub(started)) / float64(time.Millisecond))})
			}
			if err == nil {
				samples = append(samples, sample)
			}
			return err
		})
		if err != nil {
			l.Manager.RecordPerformance(binding, samples)
			return err
		}
	}
	if !l.Manager.RecordPerformance(binding, samples) {
		return routerruntime.ErrCapability
	}
	return nil
}

func sampleLocalPerformance(ctx context.Context, endpoint, model string, usage ...func([]byte)) (routerruntime.PerformanceSample, error) {
	prompt := strings.Repeat("This is fixed synthetic performance material. No personal information or real application request is included. ", 56)
	body, _ := json.Marshal(map[string]any{"model": model, "messages": []map[string]string{{"role": "system", "content": "Write a detailed numbered list with at least twenty items. Continue until the output limit. Do not use tools."}, {"role": "user", "content": prompt + "List twenty practical ways to organize a desk, with a full sentence for each item."}}, "stream": true, "stream_options": map[string]bool{"include_usage": true}, "max_tokens": 96, "temperature": 0, "cache_prompt": false, "chat_template_kwargs": map[string]bool{"enable_thinking": false}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return routerruntime.PerformanceSample{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := routerprocess.NewHTTPClient()
	defer client.CloseIdleConnections()
	started := time.Now()
	response, err := client.Do(req)
	if err != nil {
		return routerruntime.PerformanceSample{}, errors.New("local_performance_request_failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return routerruntime.PerformanceSample{}, errors.New("local_performance_request_failed")
	}
	return readPerformanceStream(response.Body, started, time.Now, usage...)
}

func readPerformanceStream(body io.Reader, started time.Time, now func() time.Time, usage ...func([]byte)) (routerruntime.PerformanceSample, error) {
	var sample routerruntime.PerformanceSample
	var first, last time.Time
	reader := io.LimitReader(body, (256<<10)+1)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	var bytesRead int
	finished := false
	completed := false
	for scanner.Scan() {
		line := scanner.Text()
		bytesRead += len(line) + 1
		if bytesRead > 256<<10 {
			return sample, errors.New("local_performance_response_invalid")
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			finished = true
			break
		}
		var event struct {
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage struct {
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			return sample, errors.New("local_performance_response_invalid")
		}
		for _, choice := range event.Choices {
			if choice.FinishReason == "stop" || choice.FinishReason == "length" {
				completed = true
			}
			if choice.Delta.Content != "" {
				last = now()
				if first.IsZero() {
					first = last
				}
			}
		}
		var rawEvent struct {
			Usage json.RawMessage `json:"usage"`
		}
		_ = json.Unmarshal([]byte(data), &rawEvent)
		if len(rawEvent.Usage) > 0 && string(rawEvent.Usage) != "null" {
			for _, observe := range usage {
				observe(rawEvent.Usage)
			}
		}
		if event.Usage.CompletionTokens > 0 {
			sample.Tokens = event.Usage.CompletionTokens
		}
	}
	if scanner.Err() != nil || !finished || !completed {
		return sample, errors.New("local_performance_response_invalid")
	}
	if !first.IsZero() {
		sample.FirstTokenMS = float64(first.Sub(started)) / float64(time.Millisecond)
		if elapsed := last.Sub(first).Seconds(); elapsed > 0 {
			sample.TokensPerSecond = float64(max(0, sample.Tokens-1)) / elapsed
		}
	}
	return sample, nil
}
