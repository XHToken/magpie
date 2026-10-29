# 端云协同 Privacy Redaction and Reversible Restoration

## Purpose

Detect and redact sensitive PII, credentials, and user-defined confidential
terms from outbound cloud requests across all supported wire protocols, and
reversibly restore placeholders in both non-streaming and streaming responses.

## Entry Points

- Redaction engine & wire rewriters (`internal/routerredact/`):
  - `policy.go`: `Config`, `DetectorConfig`, `CustomEntry`, `DefaultConfig`,
    `ValidateConfig`
  - `engine.go`: `Engine`, `NewEngine`, `Session`, `Result`, `Match`
  - `detectors.go`: built-in detectors (`email`, `phone_cn`, `id_card_cn`,
    `bank_card`, `ip_address`, `api_secret`, `jwt_token`, `private_key`)
  - `stream.go`: `StreamRestorer` (chunk-safe placeholder restoration across
    SSE token boundaries)
  - `wire.go`, `wire_parse.go`, `wire_decode.go`: protocol-aware request
    redaction (`RedactWireRequest`), response restoration
    (`RestoreWireResponse`), and SSE stream wrapping (`WrapSSEStream`)
- Service HTTP handlers: `internal/routerservice/redaction.go`
  (`handleRedaction`, `handleRedactionPreview`)
- Frontend UI: `internal/routerservice/assets/routing-groups-editor.js` for
  group redaction settings and `observations.js` for request observations.
  The local-model page does not display redaction diagnostics/acceptance or
  provide a bulk intent-disable action. The authenticated `/api/redaction`
  management API and saved application/group redaction requirements remain.

## Architecture

### Policy Configuration (`routerredact.Config`)

Redaction settings live under `redaction` in `config.json`:

- `Enabled bool`: master switch for outbound cloud redaction.
- `Mode string`: `"placeholder"` (replaces sensitive spans with reversible
  tokens such as `[REDACTED_EMAIL_1]` and restores them in responses) or
  `"mask"` (irreversible masking).
- `RestoreResponses bool`: whether placeholders in upstream cloud responses are
  restored to their original values before returning to the caller.
- `Detectors map[string]DetectorConfig`: per-detector enablement for built-in
  patterns (`email`, `phone_cn`, `id_card_cn`, `bank_card`, `ip_address`,
  `api_secret`, `jwt_token`, `private_key`).
- `CustomEntries []CustomEntry`: user-defined sensitive terms or regex patterns
  with optional replacement aliases and category labels.

### Request Inspection, Smart-Split Signal, and Outbound Redaction

1. **Sensitivity Signal for Routing Groups (`smart_split` & `has_sensitive`)**:
   - During routing-group evaluation (`internal/routerservice/group_routing.go`),
     the redaction engine inspects the request payload.
   - Under `smart_split`, requests containing sensitive matches (`hasSensitive`)
     can be steered according to group rules or redacted before cloud dispatch.
2. **Protocol-Aware Wire Redaction (`wire.go`, `wire_parse.go`)**:
   - `RedactWireRequest` traverses user/system text fields, tool arguments, and
     tool outputs across OpenAI Chat, OpenAI Responses, Anthropic Messages, and
     Gemini Native JSON payloads without corrupting structural fields (such as
     `model`, `role`, tool names, or JSON schema keys).
   - Generates deterministic per-request session placeholders
     (`[REDACTED_<CATEGORY>_<N>]`) and records match counts and categories in
     `routerredact.Result` for `routerobs` telemetry.

### Non-Streaming and Streaming Restoration (`wire_decode.go`, `stream.go`)

- **Non-Streaming (`RestoreWireResponse`)**: Scans assistant message content and
  tool-call arguments in the upstream JSON response and substitutes session
  placeholders back with their original values.
- **Streaming SSE (`StreamRestorer`, `WrapSSEStream`)**: Buffers only the minimal
  trailing prefix needed when a `[` placeholder spans multiple SSE chunks,
  emitting restored text deltas in real time without breaking SSE framing.
  Responses reasoning summaries use separate `summary_index` channels; closing
  one summary must not close another, and synthesized flush events retain that
  summary's index.

## Invariants

### Nested groups and request-scoped requirements

`routerconfig.ResolveActiveGroupExpansion` returns both model members and
privacy sources. `routerservice.groupPrivacyPlan` merges requirements from the
application, every active group in the selected expansion, and the matched rule's
group target before ordering or deduplication. A requirement protects every cloud
candidate in that request, including sibling fallbacks. Hidden, disabled and
excluded groups contribute nothing; incomplete expansion fails closed.

The server-owned context plan survives candidate target replacement. Required
models are selected from each source group's own effective member order; unequal
model IDs (including different quantizations) return `redaction_policy_conflict`.
A source with no local member returns `redaction_model_missing`. These are 409
configuration errors. Unavailable/invalid preparation returns 503
`redaction_unavailable` and terminates both the candidate loop and patience
retries. Ordinary local judgment/execution failures can still select a cloud
candidate, which must pass the same privacy gate.

`prepareRequiredRedaction` shares one preparation deadline across candidates
(at most 30 seconds, bounded by the request deadline) and pins routing versions
before and after extraction. It reuses the existing exact extractor/cache
identities; inheritance does not change extraction material or grant capability
acceptance. Protocol/target preparation still precedes redaction, so provider
extra body is checked. Prepared bodies are not exchanged between providers.

Saved `localRedaction` intent is not automatically cleared when a model/member
disappears. New enablement without a consistent local model is rejected by the
management save API. `/api/routing-groups?preview=1` accepts an authenticated
POST for read-only draft policy resolution. Group views expose `redactionPolicy`
with own/inherited requirements, source group IDs, model, local-member presence,
rule dependence and fixed invalid-reason codes. Rule-dependent summaries are
static; only the matched rule affects an actual request.

Request policy state is persisted separately from model call status; see the
[request ledger](wefto-request-ledger.md). Local completion without any cloud
preparation is `not_sent`, not proof of successful cloud redaction. Relevant
regressions are `group_redaction_policy_test.go`, `groups_privacy_test.go`, and
the routing-group browser suite.

### Local model preparation budget and policy identity

The local-model preparation path uses
[`Pipeline.Prepare`](../../internal/routerredact/pipeline.go),
[`RuntimeExtractor.Extract`](../../internal/routerredact/runtime.go) and the
service-owned runtime Manager. Their default redaction deadline is 30 seconds,
defined by `routerruntime.DefaultRedactionBudget`. The deadline covers request
preparation, runtime queueing and every chunk/batch; batching does not renew it.
A shorter caller deadline or cancellation still wins. Invalid model results
and incomplete preparation block cloud dispatch.
Rule matching is part of that complete scan: an initial or subsequent matcher
failure, including a regular-expression timeout, returns the fixed safe code
`redaction_extraction_failed` and blocks dispatch. Matcher diagnostics containing
source text are not propagated to clients or observations. The management API
reports the Manager's actual preparation budget for both formal and trial paths.

[`PrivacySpecification`](../../internal/routerredact/privacy_specification.go)
versions the strict parsing policy as `privacy-go-v6` (explicit trials use
`privacy-trial-v2`). Its configuration hash includes the 30-second budget and
`coordinateRecoveryPolicy=unique-exact-substring-valid-coordinates-v1`.
[`Parse`](../../internal/routerredact/privacy_parse.go) requires valid supplied
coordinate bounds, but may recover a mismatched span from one unique exact
source substring with valid ASCII boundaries. Uniqueness includes overlapping
matches, including multibyte and non-BMP text. Missing or ambiguous matches,
invalid bounds, duplicates and other invalid findings still reject the result.
This declaration records existing parser behavior; it adds no new recovery.

The configuration and suite identities change with this policy, so prior
acceptance/cache identities cannot substitute for new policy evidence. Model,
runtime artifact, prompt and schema identities remain tied to their own inputs;
the policy update does not require new weights or grant capability acceptance.

Explicit experimental privacy preparation accepts the current `spark-x2.6-4B`
and `spark-x2.6-4B-Q8_0` variants, using independent pinned model fingerprints
and the same `privacy-trial-v2` material, parser and budget. It still requires the trusted
startup permission (enabled by default by the product CLI) and exact current binding; adding a model grants no formal
redaction quality acceptance or automatic privacy enablement.

Both X2.6 catalog entries explicitly declare `redactionTrial: true` and the
purpose `public_execution_and_redaction_validation_only`. The specification
and runtime invoker share `Model.SupportsRedactionTrial` for admission; a
descriptive purpose string is not an authorization input. This metadata change
updates the bound model fingerprints, invalidating old capability/cache
bindings, while retaining the same pinned weights, template and runtime bytes.
Existing downloads need no replacement. The original extraction, restoration,
30-second budget and failure-blocking paths remain in use for both variants.
Saved installation manifests may retain historical descriptive metadata until
refreshed by installation; admission uses the service's fixed catalog, not that
saved purpose string. Historical held-out evaluation names identify their
material provenance and do not restrict the production model allowlist.

### Plaintext lifetime

- Mapping/session caches are held in memory. The separate service-owned
  `routerledger.RedactionObservationStore` persists the latest user input,
  redacted input and replacement values in the private
  `<state>/redaction_observations.sqlite` database for the authenticated
  management view. Its default retention is seven days and 500 request IDs.
  Queries exclude expired records; physical pruning (with `PRAGMA secure_delete=ON`)
  runs on database open, record writes, queries, and periodic background expiration.
  Repeated observations for one request update that record without evicting
  another request at capacity. The management clear operation deletes the
  stored observations. Business request/usage ledgers remain separate.
- Local engine executions (`local/*`) stay on-device and do not send data to
  external cloud endpoints.
