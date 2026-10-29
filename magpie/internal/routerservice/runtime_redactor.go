package routerservice

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"crypto/sha256"
	"encoding/json"
	"github.com/yetone/magpie/internal/cloudcodec"
	"github.com/yetone/magpie/internal/routerredact"
	"github.com/yetone/magpie/internal/routerruntime"
)

// RedactionService owns one production engine for all request policy sources.
type RedactionService struct {
	sessionKey   []byte
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	local        *LocalModels
	binding      routerredact.ModelBinding
	invoker      routerredact.RuntimeInvoker // trusted integration fixtures only
	experimental bool
	active       *routerredact.Pipeline
	activeSP     *routerredact.PrivacySpecification
	retired      []*routerredact.Pipeline
	refs         map[*routerredact.Pipeline]int
	totals       routerredact.Stats
	closed       bool
}

func newRedactionService(local *LocalModels, experimental ...bool) *RedactionService {
	ctx, cancel := context.WithCancel(context.Background())
	return &RedactionService{ctx: ctx, cancel: cancel, local: local, experimental: len(experimental) > 0 && experimental[0], refs: map[*routerredact.Pipeline]int{}}
}
func routingVersions(spec *routerredact.PrivacySpecification) RoutingVersions {
	v := spec.RoutingVersions()
	return RoutingVersions{v.LocalRuntimeFingerprint, v.LocalModelFingerprint, v.LocalTemplateFingerprint, v.RedactionPromptVersion, v.RedactionSchemaVersion, v.ConfigVersion}
}
func (r *RedactionService) specification(ctx context.Context) (*routerredact.PrivacySpecification, error) {
	if r.closed {
		return nil, ErrRedactionUnavailable
	}
	if r.local == nil || r.local.Manager == nil {
		if r.invoker == nil {
			return nil, ErrRedactionUnavailable
		}
		return routerredact.NewEngineSpecification(r.binding, false)
	}
	identity, settings := r.local.Manager.SnapshotBinding(ctx)
	spec, err := routerredact.NewEngineSpecification(routerredact.ModelBinding{ModelID: identity.ModelID, RuntimeID: identity.RuntimeID, ContextTokens: settings.Effective().ContextTokens, ExecutionPlan: identity.ExecutionPlan}, r.experimental)
	if err != nil || spec.RuntimeFingerprint() != identity.ModelFingerprint {
		return nil, ErrRedactionUnavailable
	}
	return spec, nil
}
func (r *RedactionService) currentLocked(ctx context.Context) (*routerredact.Pipeline, *routerredact.PrivacySpecification, *routerredact.Pipeline, error) {
	spec, err := r.specification(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if r.active != nil && r.activeSP.Versions() == spec.Versions() {
		return r.active, r.activeSP, nil, nil
	}
	var invoker routerredact.RuntimeInvoker = r.invoker
	if invoker == nil {
		invoker = r.local.Manager
		if r.experimental {
			invoker, err = r.local.Manager.NewExperimentalRedactionInvoker(spec.RuntimeFingerprint(), spec.CapabilitySuite())
			if err != nil {
				return nil, nil, nil, ErrRedactionUnavailable
			}
		}
	}
	budget := spec.Budget()
	if r.local != nil && r.local.Manager != nil {
		if b := r.local.Manager.RedactionBudget(); b > 0 && b < budget {
			budget = b
		}
	}
	pipeline := routerredact.New(routerredact.Options{Extractor: routerredact.NewHybridExtractor(invoker, spec), Budget: budget, ReversibleAmounts: true})
	var closeOld *routerredact.Pipeline
	if old := r.active; old != nil {
		old.Clear()
		if r.refs[old] > 0 {
			r.retired = append(r.retired, old)
		} else {
			mergeRedactStats(&r.totals, old.Stats())
			closeOld = old
		}
	}
	r.active, r.activeSP = pipeline, spec
	return pipeline, spec, closeOld, nil
}
func (r *RedactionService) current() (*routerredact.Pipeline, *routerredact.PrivacySpecification, error) {
	r.mu.Lock()
	p, s, old, err := r.currentLocked(context.Background())
	r.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return p, s, err
}
func (r *RedactionService) RoutingVersions() RoutingVersions {
	r.mu.Lock()
	s, err := r.specification(context.Background())
	r.mu.Unlock()
	if err != nil {
		return RoutingVersions{}
	}
	return routingVersions(s)
}
func redactProtocol(value string) (routerredact.Protocol, error) {
	switch value {
	case "chat":
		return routerredact.Chat, nil
	case "responses":
		return routerredact.Responses, nil
	case "anthropic":
		return routerredact.Anthropic, nil
	}
	return "", errors.New("redaction_protocol_unsupported")
}
func (r *RedactionService) release(p *routerredact.Pipeline) {
	r.mu.Lock()
	r.refs[p]--
	var old *routerredact.Pipeline
	if r.refs[p] == 0 {
		delete(r.refs, p)
		for i, retired := range r.retired {
			if retired == p {
				r.retired = append(r.retired[:i], r.retired[i+1:]...)
				mergeRedactStats(&r.totals, p.Stats())
				old = p
				break
			}
		}
	}
	r.mu.Unlock()
	if old != nil {
		old.Close()
	}
}
func (r *RedactionService) Prepare(ctx context.Context, req RedactionRequest) (PreparedCloudRequest, error) {
	return r.prepare(ctx, req, nil)
}

// prepare takes one immutable binding. Later candidates must match it; policy
// source flags never select another specification or renew the deadline.
func (r *RedactionService) prepare(ctx context.Context, req RedactionRequest, expected *RoutingVersions) (PreparedCloudRequest, error) {
	started := time.Now()
	budget := routerruntime.DefaultRedactionBudget
	if r.local != nil && r.local.Manager != nil {
		if b := r.local.Manager.RedactionBudget(); b > 0 && b < budget {
			budget = b
		}
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	stop := context.AfterFunc(r.ctx, cancel)
	defer func() { stop(); cancel() }()
	target, err := redactProtocol(req.TargetProtocol)
	if err != nil {
		return PreparedCloudRequest{}, err
	}
	client, err := redactProtocol(req.SourceProtocol)
	if err != nil {
		return PreparedCloudRequest{}, err
	}
	r.mu.Lock()
	transient := false
	p, s, old, err := r.currentLocked(ctx)
	if err != nil {
		source := req.SourceBody
		if len(source) == 0 && client == target {
			source = req.Body
		}
		view, e := cloudcodec.NewUserTextView(string(client), source)
		noInput := e == nil
		if noInput {
			for _, text := range view.Texts() {
				if text != "" {
					noInput = false
					break
				}
			}
		}
		if noInput {
			if spec, e := r.specification(ctx); e == nil {
				transient = true
				p = routerredact.New(routerredact.Options{})
				s = spec
				old = nil
				err = nil
				r.retired = append(r.retired, p)
			}
		}
	}
	if err == nil {
		if expected != nil && *expected != routingVersions(s) {
			err = ErrRedactionUnavailable
		}
		if work := redactionWork(ctx); work != nil && work.plan.RequiredModelID != "" && r.local != nil && r.local.Manager != nil {
			id, _ := r.local.Manager.SnapshotBinding(ctx)
			if !strings.EqualFold(work.plan.RequiredModelID, id.ModelID) {
				err = ErrRedactionUnavailable
			}
		}
		if err == nil {
			r.refs[p]++
		}
	}
	if err != nil && transient {
		r.retired = r.retired[:len(r.retired)-1]
		old = p
	}
	r.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if err != nil {
		return PreparedCloudRequest{}, err
	}
	ok := false
	defer func() {
		if !ok {
			r.release(p)
		}
	}()
	source := req.SourceBody
	if len(source) == 0 {
		if client != target {
			return PreparedCloudRequest{}, ErrRedactionUnavailable
		}
		source = req.Body
	}
	userTarget := req.UserTarget
	if userTarget == nil {
		if client != target {
			return PreparedCloudRequest{}, ErrRedactionUnavailable
		}
		view, e := cloudcodec.NewUserTextView(string(client), source)
		if e != nil {
			return PreparedCloudRequest{}, ErrRedactionUnavailable
		}
		userTarget, e = view.BindTarget(req.Body, func(b []byte) ([]byte, error) { return b, nil })
		if e != nil {
			return PreparedCloudRequest{}, ErrRedactionUnavailable
		}
	}
	var user *routerredact.UserSnapshot
	work := redactionWork(ctx)
	if work != nil && work.session.Verified {
		req.SessionID = work.session.ID
		req.SessionVerified = true
	}
	view, e := cloudcodec.NewUserTextView(string(client), source)
	if e != nil {
		return PreparedCloudRequest{}, ErrRedactionUnavailable
	}
	if !userTarget.MatchesSource(view) {
		return PreparedCloudRequest{}, ErrRedactionUnavailable
	}
	material, _ := json.Marshal([]any{req.AppID, client, view.Identities(), view.Texts()})
	sourceIdentity := sha256.Sum256(material)
	if work != nil && work.userSnapshot != nil {
		if work.sourceIdentity != sourceIdentity {
			return PreparedCloudRequest{}, ErrRedactionUnavailable
		}
		user = work.userSnapshot
	} else {
		user, err = p.ScanUserInput(ctx, routerredact.Input{RequestID: req.RequestID, AppID: req.AppID, Protocol: client, Operation: req.Operation, SessionID: req.SessionID, SessionVerified: req.SessionVerified, Versions: s.Versions(), Body: source, ReservedBody: req.Body})
		if err != nil {
			return PreparedCloudRequest{}, err
		}
		if work != nil {
			work.userSnapshot = user
			work.sourceIdentity = sourceIdentity
		} else {
			defer user.Close()
		}
	}
	prepared, err := user.PrepareTargetContext(ctx, target, req.Body, userTarget)
	if err != nil {
		return PreparedCloudRequest{}, err
	}
	if r.RoutingVersions() != routingVersions(s) || ctx.Err() != nil {
		prepared.Close()
		return PreparedCloudRequest{}, ErrRedactionUnavailable
	}
	body, err := prepared.Body()
	if err != nil {
		prepared.Close()
		return PreparedCloudRequest{}, err
	}
	meta, _ := prepared.ObservationMeta()
	meta.DurationMS = time.Since(started).Milliseconds()
	var once sync.Once
	ok = true
	return PreparedCloudRequest{Body: body, ObservationMeta: &meta, Versions: routingVersions(s), ModelID: s.ModelID(), EngineVersion: routerredact.EngineVersion,
		Restore: func(ctx context.Context, protocol, _ string, body []byte) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			actual, err := redactProtocol(protocol)
			if err != nil || actual != client {
				return nil, errors.New("redaction_protocol_mismatch")
			}
			return prepared.Snapshot.RestoreResponse(actual, body)
		},
		RestoreSSE: func(ctx context.Context, protocol string, input io.Reader, output io.Writer) error {
			actual, err := redactProtocol(protocol)
			if err != nil || actual != client {
				return errors.New("redaction_protocol_mismatch")
			}
			return prepared.Snapshot.RestoreSSE(ctx, actual, input, output)
		},
		Close: func() error { once.Do(func() { prepared.Close(); r.release(p) }); return nil },
	}, nil
}
func mergeRedactStats(dst *routerredact.Stats, src routerredact.Stats) {
	if dst.Failures == nil {
		dst.Failures = map[string]uint64{}
	}
	dst.NewUserCharacters += src.NewUserCharacters
	dst.NewUserChunks += src.NewUserChunks
	dst.ReusedUserFragments += src.ReusedUserFragments
	dst.LocalInferenceCalls += src.LocalInferenceCalls
	dst.NoUserInput += src.NoUserInput
	dst.KnownValuesOnly += src.KnownValuesOnly
	dst.Processed += src.Processed
	dst.Entities += src.Entities
	dst.CacheHits += src.CacheHits
	dst.Blocked += src.Blocked
	dst.DurationMS += src.DurationMS
	for code, n := range src.Failures {
		dst.Failures[routerredact.SafeFailureCode(code)] += n
	}
}
func (r *RedactionService) Stats() routerredact.Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	var stats routerredact.Stats
	mergeRedactStats(&stats, r.totals)
	if r.active != nil {
		mergeRedactStats(&stats, r.active.Stats())
	}
	for _, p := range r.retired {
		mergeRedactStats(&stats, p.Stats())
	}
	return stats
}
func (r *RedactionService) close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.cancel()
	pipelines := append([]*routerredact.Pipeline(nil), r.retired...)
	if r.active != nil {
		pipelines = append(pipelines, r.active)
	}
	r.mu.Unlock()
	for _, p := range pipelines {
		p.Close()
	}
}
func (r *RedactionService) Close() error { r.close(); return nil }
