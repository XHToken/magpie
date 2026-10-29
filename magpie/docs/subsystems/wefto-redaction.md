# 端云协同 user input redaction

## Responsibility and ownership

Application `redactionEnabled`, active routing-group `localRedaction`, nested
groups and matched rule targets contribute requirements to one server-owned
request plan. They use the same production engine and do not choose different
algorithms. The user-input scope contract supersedes the older full-request scope.

`routerservice.RedactionService` is created by the service owner and shared through
`ModelOptions.Redaction`. It owns one current pipeline, a bounded history cache,
aggregate statistics, cancellation and retiring pipelines. There is no
group-instance global cache. A group's compatibility execution scope does not replace the engine.

Sources of truth:

- [request policy](../../internal/routerservice/group_redaction_policy.go):
  `groupPrivacyPlan`, `prepareRequiredRedaction`, `redactionSnapshot`;
- [service lifetime and binding](../../internal/routerservice/runtime_redactor.go):
  `RedactionService`, `prepare`, `Close`;
- [engine specification and extraction](../../internal/routerredact/engine.go):
  `NewEngineSpecification`, `HybridExtractor`;
- [original-protocol user view and source sidecar](../../internal/cloudcodec/user_input.go),
  [user scan and known-value application](../../internal/routerredact/user_input.go),
  [legacy diagnostic wire parsing](../../internal/routerredact/wire_parse.go),
  [pipeline/cache](../../internal/routerredact/pipeline.go),
  [mapping](../../internal/routerredact/mapping.go),
  [strict parser](../../internal/routerredact/privacy_parse.go) and
  [JSON/SSE restoration](../../internal/routerredact/stream.go).

## Requirements and model selection

Requirements combine with OR before candidate sorting or deduplication. Switching
candidates, affinity, patience retries and restoring an earlier target cannot
clear the request plan. Hidden, disabled and excluded groups contribute nothing;
incomplete expansion fails closed. Only matched rule targets add requirements.

Application-only requirements use the Manager's selected local model. Groups
constrain the model to their own first effective local member. Different group
models, including different quantizations, return 409 `redaction_policy_conflict`.
A required group with no local member returns 409 `redaction_model_missing`.
A mismatch with the actual selected model, unavailable runtime, invalid output or
failed preparation blocks cloud dispatch with 503 `redaction_unavailable`.

Overlapping application and group requirements mean one preparation for each
final target request. Different cloud targets prepare their own final bodies;
model calls within a preparation may be batched.

Application requirements protect cloud egress. Existing direct-group local
preparation/restoration is retained through `RedactLocal` in the request plan;
inheritance does not change the parent's local execution scope. No local model
or host endpoint is selected or changed automatically.

## Final request and response boundary

User identification uses the original validated inbound protocol. Chat selects
only `role=user` content strings/direct text blocks; Responses selects string
`input` or ordinary `message` items explicitly marked `role=user`; Anthropic
selects direct user text, excluding nested `tool_result` content. All user turns
participate, including earlier turns when the request ends with a tool callback.
User-pasted code, attachments represented as text and host context concatenated
into that same string are scanned in full.

System/developer/instructions, tools/schema/keys, host/accounting metadata,
assistant messages, tool arguments and tool outputs never enter rules,
dictionaries or the local extraction model for new entity discovery. Existing
JSON/protocol/capability limits and the privacy text-only subset remain enforced.
A secret first introduced by a system prompt or tool is outside this scope.

One request owns a `UserSnapshot` containing processed fragments and their
private mappings. Candidate protocols and provider options are prepared
separately. `UserTextView.BindTarget` passes unpredictable internal stand-ins
through the same codec and ExtraBody expansion. It verifies each source survives,
including multiple output locations and merged-string intervals, and verifies
that rendering the original fragments reproduces the real final target tree.
Source positions never come from client metadata or equality searches over
business text. The stand-ins are never sent. Lost/overwritten source fragments
and explicit reinjection through ExtraBody fail closed with the fixed business
error `redaction_unavailable`; configuration already forbids conversation/target
overrides independently.

Assistant text, tool results and string leaves inside safely parsed tool
arguments reuse only exact originals identified from user input. Replacement is
single pass, longest match first, with case preserved. Boundary version
`exact-longest-boundary-v1` requires Unicode word boundaries for values shorter
than four code points. Longer values use ASCII word-neighbor checks at ASCII
word edges, allowing a phone number adjacent to Chinese prose while excluding
an ASCII identifier suffix. Keys, numbers, tool names, IDs,
system/schema and unknown values are untouched. The trie matcher checks the
shared context while applying mappings. Literal placeholder collisions reserve
fresh IDs; only tokens actually emitted in allowed target values authorize
response restoration. A missing allocated mapping fails closed.

A required new scan remains blocked while inference is unavailable. A request
with no user text performs no inference, even without an available model; an
unchanged selected binding is still required. Existing trusted, tool-only
mapping application can finish with that binding while disabled. Full user-cache
hits conservatively retain the extractor's current admission check. Neither
case acquires or waits for a model slot merely to attest completion.

The executor receives only the prepared final body. Adaptation to the client's
protocol precedes restoration. Each Prepared request owns an independent mapping
snapshot; JSON and SSE use the same snapshot, including split tokens, independent
tool channels and reasoning summary channels. A restoration error terminates the
response. Output commitment still prevents selecting a second result.

Native token counting, connection checks, titles and ordinary generation obey
the same cloud egress rule. Token counting is not exempt because it sends data.

## Unified detection and strict validation

Rules and the dictionary scan complete uncached user fields; the local model independently
scans every planned nonempty chunk. Results combine through a deterministic union
of overlapping sensitive spans, preserving their uncovered bytes. Rule matches
do not excuse missing model completion. Non-sensitive `public_information` and
`job_title` findings are ignored only after strict validation; they cannot erase
rule/dictionary sensitive findings.

The engine retains all 49 legacy model labels and adds IP, MAC and hostname
labels. Sensitive personal dates, business ratios and short secrets are not
silently removed by the legacy group detector's heuristic filters. The legacy
model-only specification and legacy orchestrator remain available for diagnostic
and fixture baselines; production has no fallback to either algorithm.

Output requires exactly one complete response with `finish_reason=stop`, an
assistant content string, complete exact field coverage, known labels, finite
confidence and valid Unicode coordinates. Duplicate keys, truncation, ambiguous
or missing source substrings, invalid bounds and invalid findings reject the
whole preparation. Coordinate recovery permits only the versioned unique exact
substring rule with valid supplied bounds and ASCII boundaries.

Sensitive amounts now use reversible placeholders. Group requests no longer
implicitly generalize amounts. The engine preserves the original fine category
in observations. Restoration recovers echoed original values, not arithmetic
performed on hidden operands. Numeric JSON scalars retain their existing wire
contract; this change does not claim general scalar masking support.

## Chunks, deadline and binding

The engine uses approximately 1800 Unicode code points per chunk with 120 points
of overlap, at most 64 chunks per new scan, and strict batches of at most 16
fields / 2048 code points. The application-only 2048-point whole-field limit no
longer applies. The runtime still validates token capacity and reserves output;
context overflow fails preparation rather than truncating input.

Outer limits remain 8 MiB and 4096 entities; the 8192-field and 64-chunk scan limits count user fragments only. Every nonempty
planned chunk must finish; an over-limit plan fails before model calls. Empty
fields need no inference because they contain no sensitive bytes. Full character
coverage is an engineering guarantee, not proof of recall for long entities
crossing chunk boundaries.

One deadline covers preparation, loading, queueing, rules, all model batches and
candidate re-preparation: at most 30 seconds, bounded by the request deadline
and any shorter Manager budget. A retry or another chunk cannot renew it.

`NewEngineSpecification` hashes actual prompt/schema, label/action policy,
rule/dictionary material, builtin definitions, normalization, chunking, limits,
amount handling and validation/merge policy. Production uses `privacy-user-input-v1`
or `privacy-user-input-trial-v1` suites. Scope, extraction, mapping boundaries,
server-issued session/cache policy and target restoration authority are included
in the material identity. Old full-request caches and suites cannot authorize
this scope. Old model-only acceptance cannot authorize
the new extractor. The Manager verifies the same model fingerprint and suite;
experimental permission does not grant formal quality acceptance.

The first preparation binds an immutable specification; later candidates must
match. Model/context/backend changes before dispatch reject that preparation.
Previously prepared response snapshots remain independent of cache rotation.

## Cache, observations and shutdown

Cache keys isolate authenticated application, server-verified session, source
user ordinals/text-block ordinals, exact user fingerprints, fixed scope and
model/material binding. User identification is independent of target provider,
policy source flags, system prompts, tools and ordinary assistant/tool changes.
Only an unchanged user prefix is reused; edits, deletion, reordering and forks
are rescanned. Every outbound history is still rewritten on a cache hit.

There is no verified host-session adapter in this checkout. Body session IDs,
metadata, trace IDs, message counts and identical first-user text grant no trust.
The service can issue `X-Router-Redaction-Session`: a random nonce, timestamp and
HMAC-SHA256 bound to the authenticated app and this service instance, valid for
15 minutes. A host may echo it to reuse its private cache; missing/invalid,
cross-app or pre-restart handles use a fresh request session and cannot read
another session's mappings. This requires no host endpoint/configuration change.
Internal trusted adapters may supply `SessionVerified`; unverified seed keys are
never published. The history cache remains bounded to 128 entries, 16 MiB and
15 minutes, with epoch invalidation and independent response snapshots.

`/api/redaction` is authenticated and reports the shared service's aggregate
preparation statistics, actual selected model, engine version, readiness and
trial/formal state. Readiness does not mean every application/group is enabled.
Request policy observations retain application/group sources, required model,
actual prepared model and engine version separately from cloud attempts.
Successful preparation also records `scopeVersion`, `scanDisposition`
(`scanned`, `cache_hit`, `known_values_only`, `no_user_input`), newly scanned
user characters/chunks, reused fragments, actual inference calls and preparation
milliseconds. Failed calls retain separate safe runtime observations rather than
claiming a successful zero-scan result. Actual reported tokens and combined
queue/load timing remain in local-call records; absent usage is unknown.
No-user-input is a scope disposition, not a claim that all content is private.
The routing-group editor provides the User input redaction switch and explains
that system/tool discoveries are excluded. Application Details has no redaction
control. The application flag remains off by default; existing saved values and
the management API remain compatible, and page visits do not rewrite them.

Mapping/cache data is in memory. The authenticated latest-user-input comparison
continues to retain original text, redacted text and replacement values in
`<state>/redaction_observations.sqlite`, at most 500 request IDs / seven days.
It does not display the entire scanned request. Pruning and clear operations
retain existing secure-delete behavior. Ordinary logs contain safe metadata.

Close stops new work, cancels in-flight preparation and closes pipeline expiry
loops. Prepared snapshots close after response restoration; repeated Close is
safe. No model process is owned by the redaction service: it uses the shared
runtime Manager.

## Verification

`user_input_test.go` in codec, redaction and service covers original-source
projection, the nine conversion pairs, cache trust, callback zero inference,
known-value boundaries, literal/unused-token authority, large tool schemas,
request snapshot reuse and configuration conflicts. `engine_test.go` covers long fields, exact amounts, strict chunk coverage, scope
isolation, bounds and new acceptance identities. `unified_redaction_test.go`
compares application, group and combined paths on both X2.6 variants, verifies
shared statistics, cancellation and concurrent ownership. Existing group-policy,
final-boundary, protocol, cache and restoration regressions remain relevant.

Run affected package tests and race checks under `testenv` isolation. Synthetic
local/cloud transports prove engineering behavior; real-model trials and held-out
quality evidence must be reported separately. An experimental engine remains
experimental after the engineering tests pass.
