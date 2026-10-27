package routerruntime

import (
	"math"
	"time"
)

type PerformanceSample struct {
	Tokens          int     `json:"tokens"`
	TokensPerSecond float64 `json:"tokensPerSecond"`
	FirstTokenMS    float64 `json:"firstTokenMs"`
}

type Performance struct {
	State           string              `json:"state"`
	Fingerprint     string              `json:"fingerprint,omitempty"`
	Samples         []PerformanceSample `json:"samples,omitempty"`
	TokensPerSecond float64             `json:"tokensPerSecond,omitempty"`
	FirstTokenMS    float64             `json:"firstTokenMs,omitempty"`
	CheckedAt       time.Time           `json:"checkedAt,omitempty"`
}

func SummarizePerformance(samples []PerformanceSample) Performance {
	p := Performance{State: "unknown"}
	for _, s := range samples {
		if s.Tokens >= 32 && s.Tokens <= 96 && s.TokensPerSecond > 0 && s.FirstTokenMS >= 0 && !math.IsNaN(s.TokensPerSecond) && !math.IsInf(s.TokensPerSecond, 0) && !math.IsNaN(s.FirstTokenMS) && !math.IsInf(s.FirstTokenMS, 0) {
			p.Samples = append(p.Samples, s)
		}
	}
	if len(p.Samples) != 2 {
		return p
	}
	p.TokensPerSecond = (p.Samples[0].TokensPerSecond + p.Samples[1].TokensPerSecond) / 2
	p.FirstTokenMS = (p.Samples[0].FirstTokenMS + p.Samples[1].FirstTokenMS) / 2
	p.State = "acceptable"
	if p.TokensPerSecond < 5 || p.FirstTokenMS > 15000 {
		p.State = "slow"
	}
	return p
}

type PerformanceBinding struct {
	Identity LocalCallIdentity
	Epoch    uint64
	PID      int
}

func (m *Manager) PerformanceBinding() PerformanceBinding {
	m.mu.Lock()
	defer m.mu.Unlock()
	model, rt, err := m.resources()
	if err != nil || m.proc == nil || !m.healthy {
		return PerformanceBinding{}
	}
	return PerformanceBinding{Identity: LocalCallIdentity{ExecutionPlan: rt.Execution, ModelID: model.ID, ModelFingerprint: model.Fingerprint(rt), RuntimeID: rt.ID, RuntimeFingerprint: RuntimeFingerprint(rt)}, Epoch: m.performanceEpoch, PID: m.proc.PID()}
}

func (m *Manager) RecordPerformance(binding PerformanceBinding, samples []PerformanceSample) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	identity := binding.Identity
	model, rt, err := m.resources()
	if err != nil || identity.ModelID != model.ID || identity.ModelFingerprint != model.Fingerprint(rt) || !m.healthy || m.closed || m.stopping || m.proc == nil || binding.PID != m.proc.PID() || binding.Epoch != m.performanceEpoch {
		return false
	}
	p := SummarizePerformance(samples)
	p.Fingerprint, p.CheckedAt = identity.ModelFingerprint, m.o.Now()
	m.performance = p
	m.notifyLocked()
	return true
}

func (m *Manager) performanceReadyLocked() bool {
	return !m.o.PerformanceRequired || (m.performance.State == "acceptable" && m.performance.Fingerprint == m.fingerprintLocked())
}
