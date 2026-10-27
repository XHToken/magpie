package routerprocess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/yetone/magpie/internal/proc"
)

// GPUOptions is supplied by the runtime owner, never by a management request.
// Fit may select fewer layers, but the explicit context size is never reduced.
type GPUOptions struct {
	Device     string
	Layers     int // -1 lets the pinned engine fit layers to the device budget.
	ReserveMiB int
}

// Device is an observation from this exact verified engine's --list-devices.
// Memory is the engine's snapshot, not a reservation or an installation claim.
type Device struct {
	ID, Name              string
	TotalBytes, FreeBytes uint64
}

type AccelerationEvidence struct {
	Device       string `json:"device,omitempty"`
	Offloaded    int    `json:"offloadedLayers"`
	Total        int    `json:"totalLayers"`
	DeviceBuffer bool   `json:"deviceBuffer"`
	Verified     bool   `json:"verified"`
}

// AccelerationReporter exposes startup facts without keeping raw engine logs.
type AccelerationReporter interface {
	Acceleration() AccelerationEvidence
}

var deviceLine = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9_-]*): (.{1,256}) \(([0-9]+) MiB, ([0-9]+) MiB free\)\s*$`)
var offloadLine = regexp.MustCompile(`offloaded ([0-9]+)/([0-9]+) layers to GPU`)
var modelBufferLine = regexp.MustCompile(`(?:^|\s)([A-Za-z][A-Za-z0-9_-]*)\s+model buffer size\s*=\s*([0-9]+(?:\.[0-9]+)?) MiB`)

func parseDevices(raw string) ([]Device, error) {
	if !strings.Contains(raw, "Available devices:") {
		return nil, errors.New("local_backend_probe_invalid")
	}
	var devices []Device
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		m := deviceLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		total, te := strconv.ParseUint(m[3], 10, 40)
		free, fe := strconv.ParseUint(m[4], 10, 40)
		if te != nil || fe != nil || free > total || total == 0 || seen[m[1]] || len(devices) >= 64 {
			return nil, errors.New("local_backend_probe_invalid")
		}
		seen[m[1]] = true
		devices = append(devices, Device{ID: m[1], Name: m[2], TotalBytes: total << 20, FreeBytes: free << 20})
	}
	return devices, nil
}

// ProbeDevices starts only a verified local executable, never downloads or
// loads a model, and joins its owned process on success, failure or cancellation.
func (NativeFactory) ProbeDevices(ctx context.Context, spec Spec) ([]Device, error) {
	// Device enumeration never opens or passes a model file. Validate the engine
	// and launch contract independently of whether weights are installed.
	probeSpec := spec
	probeSpec.ModelPath = spec.Executable
	if err := validate(probeSpec); err != nil {
		return nil, err
	}
	dir, env, err := dependencyLaunch(ctx, spec)
	if err != nil {
		return nil, err
	}
	output := &boundedOutput{limit: 64 << 10}
	cmd := proc.Command(spec.Executable, "--list-devices")
	cmd.Dir, cmd.Env = dir, env
	cmd.Stdout, cmd.Stderr = output, io.Discard
	owner, err := startOwned(cmd)
	if err != nil {
		return nil, errors.New("local_backend_probe_start_failed")
	}
	done := make(chan error, 1)
	go func() { done <- owner.wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		owner.kill()
		<-done
		return nil, ctx.Err()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || output.overflow {
		return nil, errors.New("local_backend_probe_failed")
	}
	return parseDevices(output.data.String())
}

// ValidateAcceleration uses only fixed synthetic data and the already owned
// endpoint. It is an engineering check, never redaction quality acceptance.
func (NativeFactory) ValidateAcceleration(ctx context.Context, p Process) error {
	body := []byte(`{"messages":[{"role":"user","content":"Return the JSON object {\"ok\":true}."}],"temperature":0,"max_tokens":32,"stream":false,"response_format":{"type":"json_object"},"chat_template_kwargs":{"enable_thinking":false}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Endpoint()+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := NewHTTPClient()
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("accelerator_inference_failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	var completion struct {
		Choices []struct {
			Finish  string `json:"finish_reason"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err != nil || len(raw) > 64<<10 || resp.StatusCode != 200 || json.Unmarshal(raw, &completion) != nil || len(completion.Choices) != 1 || completion.Choices[0].Finish != "stop" {
		return errors.New("accelerator_inference_invalid")
	}
	var value map[string]any
	if json.Unmarshal([]byte(completion.Choices[0].Message.Content), &value) != nil || len(value) != 1 || value["ok"] != true {
		return errors.New("accelerator_inference_invalid")
	}
	return nil
}

type boundedOutput struct {
	data     strings.Builder
	limit    int
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > b.limit-b.data.Len() {
		b.overflow = true
		p = p[:max(0, b.limit-b.data.Len())]
	}
	b.data.Write(p)
	return n, nil
}

// startupEvidence consumes bounded lines only until health succeeds. Only the
// selected device's buffer and numeric offload counts survive; raw lines do not.
type startupEvidence struct {
	mu     sync.Mutex
	device string
	line   []byte
	frozen bool
	value  AccelerationEvidence
}

func (s *startupEvidence) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.frozen {
		return len(p), nil
	}
	for _, c := range p {
		if c == '\n' {
			s.consume(string(s.line))
			s.line = s.line[:0]
		} else if len(s.line) < 4096 {
			s.line = append(s.line, c)
		}
	}
	return len(p), nil
}

func (s *startupEvidence) consume(line string) {
	if m := offloadLine.FindStringSubmatch(line); m != nil {
		n, ne := strconv.Atoi(m[1])
		t, te := strconv.Atoi(m[2])
		if ne == nil && te == nil && n >= 0 && n <= t && t <= 10000 {
			s.value.Offloaded, s.value.Total = n, t
		}
	}
	if match := modelBufferLine.FindStringSubmatch(line); match != nil && strings.EqualFold(match[1], s.device) {
		size, err := strconv.ParseFloat(match[2], 64)
		if err == nil && size > 0 {
			s.value.DeviceBuffer = true
		}
	}
}

func (s *startupEvidence) snapshot(freeze bool) AccelerationEvidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	if freeze {
		s.frozen = true
		s.line = nil
	}
	v := s.value
	v.Device = s.device
	v.Verified = v.Offloaded > 0 && v.Total >= v.Offloaded && v.DeviceBuffer
	return v
}

func (p *nativeProcess) Acceleration() AccelerationEvidence {
	if p.acceleration == nil {
		return AccelerationEvidence{}
	}
	return p.acceleration.snapshot(false)
}

func gpuArguments(g *GPUOptions) []string {
	layers := strconv.Itoa(g.Layers)
	if g.Layers == -1 {
		layers = "auto"
	}
	return []string{"--device", g.Device, "--split-mode", "none", "--n-gpu-layers", layers,
		"--fit", "on", "--fit-target", strconv.Itoa(g.ReserveMiB),
		// b10828 places model/offload diagnostics at verbosity 4. The startup
		// consumer retains only numeric/device facts and discards later output.
		"--log-verbosity", "4", "--log-colors", "off"}
}

func validateGPU(s Spec) error {
	if s.GPU == nil {
		return nil
	}
	g := s.GPU
	if s.Backend == "" || s.Backend == "cpu" || !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`).MatchString(g.Device) ||
		g.Layers < -1 || g.Layers == 0 || g.Layers > 10000 || g.ReserveMiB < 256 || g.ReserveMiB > 65536 || s.ContextSize <= 0 {
		return fmt.Errorf("invalid fixed GPU launch plan")
	}
	return nil
}
