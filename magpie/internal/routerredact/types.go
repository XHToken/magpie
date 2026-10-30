// Package routerredact prepares private cloud requests and restores their replies.
// It does not decide routing or claim sensitive-entity recognition quality.
package routerredact

import (
	"context"
	"time"
)

type Protocol string

const (
	Chat      Protocol = "chat"
	Responses Protocol = "responses"
	Anthropic Protocol = "anthropic"
)

// Versions freezes the local runtime/model/template fingerprint, the redaction
// prompt/schema and the redaction configuration. Cloud or judgment versions
// cannot substitute for these independent capability/cache dimensions.
type Versions struct{ Model, Prompt, Config string }

// Input describes either the original protocol body for ScanUserInput or a
// finalized diagnostic body for legacy Prepare. Source identities and reserved
// target literals are trusted internal values, never client credentials.
type Input struct {
	AuthenticatedScope  string // Server-owned authorization domain; never body metadata.
	RecognitionMaterial string
	ReservedBody        []byte   // Target literals; only collision protection, never scanning.
	TextIdentities      []string // Server-owned original user ordinal/part identities.
	Reserved            []string // Literal token collision protection; never detection input.
	CacheScope          string
	RequestID           string
	AppID               string
	Protocol            Protocol
	Operation           string
	SessionID           string
	SessionVerified     bool   // Server-verified session source; a Profile ID is insufficient.
	SeedSessionID       string // Deprecated compatibility field; unverified seeds are ignored.
	Versions            Versions
	Body                []byte
}
type Extraction struct {
	Versions Versions
	Texts    []string
}

// Entity offsets are UTF-8 byte boundaries in one exact supplied text value.
type Entity struct {
	Field          int
	Start, End     int
	Text, Category string
}
type Result struct {
	LocalInferenceCalls, Chunks int
	Complete                    bool
	Entities                    []Entity
}
type Extractor interface {
	Extract(context.Context, Extraction) (Result, error)
}
type ExtractorFunc func(context.Context, Extraction) (Result, error)

func (f ExtractorFunc) Extract(c context.Context, in Extraction) (Result, error) {
	if f == nil {
		return Result{}, fail("redaction_unavailable")
	}
	return f(c, in)
}

// Failure never contains source text, upstream errors, IDs, or a private mapping.
type Failure struct{ Code string }

func (f *Failure) Error() string { return f.Code }
func (f *Failure) Unwrap() error {
	if f.Code == "redaction_canceled" {
		return context.Canceled
	}
	if f.Code == "redaction_timeout" {
		return context.DeadlineExceeded
	}
	return nil
}
func fail(code string) error { return &Failure{Code: code} }

type Options struct {
	Results           *ResultStore
	ReversibleAmounts bool // Unified production policy; preserve exact values in the private mapping.
	Extractor         Extractor
	Budget            time.Duration
	Experimental      bool // Explicit trial opt-in; raises only the budget ceiling to 30 seconds.
	CacheEntries      int
	CacheBytes        int
	CacheTTL          time.Duration
	Now               func() time.Time
}
