# 端云协同 user input redaction

The Windows production path now saves authenticated per-message recognition
results and rewrites known values throughout subsequent requests. The broader
[history protection plan](../redaction-history-protection-plan-20261030.md)
remains the target design; persistent session/occurrence revisions, deletion
residue handling, key rotation and recovery tooling are not implemented yet.
See the [implementation record](../redaction-history-implementation-20261030.md)
for verified behavior and release restrictions.

## Responsibility and ownership

Application `redactionEnabled`, active routing-group `localRedaction`, nested
groups and matched rule targets contribute requirements to one server-owned
request plan. They use the same production engine and do not choose different
algorithms. The user-input scope contract supersedes the older full-request scope.

`routerservice.RedactionService` is created by the service owner and shared through
`ModelOptions.Redaction`. It owns one current pipeline, a private durable recognition store,
aggregate statistics, cancellation and retiring pipelines. Legacy diagnostic pipelines retain a bounded performance cache. There is no
group-instance global recognition dictionary. A group's compatibility execution scope does not replace the engine.

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
For authenticated WorkBuddy and Loomy applications,
`NewUserHistoryViewForApp` projects the actual prompt of every user message,
excluding known host wrappers. `NewUserTextViewForApp` remains the current-only
view for title generation and observation previews. Tool results inside a user
message are never new recognition input. Malformed host framing fails closed.

[`history_input.go`](../../internal/routerredact/history_input.go) first inventories
all projected messages through one batch query. Keys bind installation, the
service-resolved application's authorization generation, exact ordered UTF-8
blocks, projection and `RecognitionMaterialID`; they exclude absolute message
indices, client IDs, cloud provider and API key. Completed zero-hit results are
required too. A valid historical result never triggers inference on restart or
cache eviction. Only a missing final ordinary user message may be recognized
in the normal request; unknown earlier messages and mixed tool callbacks require
explicit local preparation before any inference. A content edit without a
verified occurrence adapter therefore uses explicit preparation of that whole
message. The per-message admission gate coalesces identical recognition outside
service locks and SQLite transactions. Commit failure blocks the request.

The service does not infer deletion from history truncation. It never fetches
old private values solely by session ID or another request's token. This checkout
has no trusted host edit/delete lifecycle adapter; automatic cleanup of echoes
whose source was removed is **not supported**. Hosts must resynchronize such
branches with verifiable source messages. See the implementation record for this
remaining release blocker.

After recognition, a request with replacements receives a short business-model
instruction: issued tokens are supplied opaque values to use verbatim in answers
and tool arguments, rather than missing fields requiring the original values.
This instruction enters only the prepared business request, never the local
recognition request. It does not change the response's restoration authority.
Operations that need hidden properties of a value may still be impossible;
the instruction requires describing that specific limitation without inventing
originals or asking the user to resend sensitive data.

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
an ASCII identifier suffix. System/developer ordinary text and host wrapper text also receive known-value
replacement. Keys, numbers, tool names, schema and control fields that contain a
known full value block egress. The final decoded JSON check also examines nested
JSON arguments, escaped originals and unknown extensions; only actually issued
placeholder tokens are skipped. It runs again after the execution instruction
is appended. This is known-value protection, not new entity recognition of tools
or assistants. Images and unsupported protocol structures still fail closed.

[`history_boundary.go`](../../internal/routerredact/history_boundary.go) renders
all source fragments through their conversion provenance sidecar. Each request
uses a random `v1` namespace; only tokens emitted in allowed final fields enter
its restoration snapshot. Legacy tokens and tokens from other requests grant no
restoration authority. JSON/SSE parsing supports the longer namespace tokens.

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

`RecognitionMaterialID` separately hashes the actual detector/model/projection,
label maps, rules, dictionary, chunking and validation material. Output policy,
token namespaces, session compatibility and page changes do not alone invalidate
completed recognition. Outbound policy identity and final body proof are separate
request ledger fields. The strict known-value matcher has a bounded trie and
uses linear token counting/collision checks with cancellation.

The first preparation binds an immutable specification; later candidates must
match. Model/context/backend changes before dispatch reject that preparation.
Previously prepared response snapshots remain independent of cache rotation.

## Cache, observations and shutdown

[`ResultStore`](../../internal/routerredact/result_store.go) owns the independent
SQLite database under `Layout.RedactionResults()`. Its Windows DPAPI/ACL backend,
256 MiB limit and fail-closed recovery behavior are documented in
[storage](wefto-storage.md). There is no recognition TTL or automatic eviction.
Requests load only their supplied message keys. Observations cannot authorize
recognition reuse. Session handles retained for wire compatibility do not grant
dictionary access on this path.

`POST /api/redaction/history` requires the ordinary local same-origin management
authorization. `create` takes `appId`, `protocol`, original protocol `body`, and
explicit zero-based user ordinals in `selected`. It performs no inference.
`step` executes at most one strict batch (16 fields / 2048 code points), and the
page pauses between steps; `status` reports quantities and `cancel` interrupts
active preparation. Tasks select at most 100 user messages / 200000 code points,
with at most eight in-memory tasks. Only fully covered messages commit. A restart
requires resupplying the original text and reuses already committed messages.
Partially completed messages are never reusable. The task API neither sends
cloud requests nor stores full source messages on disk. Queue priority is still
the regular redaction priority; each manual step releases it after one batch.

`/api/redaction` is authenticated and reports the shared service's aggregate
preparation statistics, actual selected model, engine version, readiness and
trial/formal state. Readiness does not mean every application/group is enabled.
Request policy observations retain application/group sources, required model,
actual prepared model and engine version separately from cloud attempts.
Successful preparation also records `scopeVersion`, `scanDisposition`
(`scanned`, `persistent_reuse`, `known_values_only`, `no_user_input`), newly scanned
user characters/chunks, reused fragments, actual inference calls and preparation
milliseconds. Failed calls retain separate safe runtime observations rather than
claiming a successful zero-scan result. Actual reported tokens and combined
queue/load timing remain in local-call records; absent usage is unknown.
No-user-input is a scope disposition, not a claim that all content is private.
The routing-group editor provides the User input redaction switch and explains
that system/tool discoveries are excluded. Application Details has no redaction
control. The application flag remains off by default; existing saved values and
the management API remain compatible, and page visits do not rewrite them.

Restoration mappings are request-local. Reliable recognition entities are encrypted
on disk separately from the observations below. The request ledger also records
message reuse, known-value replacement counts and final verification state; these
fields do not claim that assistant/tool content was newly recognized. The authenticated latest-user-input comparison
continues to retain original text, redacted text and replacement values in
`<state>/redaction_observations.sqlite`, at most 500 request IDs / seven days.
It does not display the entire scanned request. Pruning and clear operations
retain existing secure-delete behavior and do not delete reliable recognition
results; the page explains the distinction. Ordinary logs contain safe metadata.
For WorkBuddy and Loomy, the comparison stores the actual current prompt rather
than the host envelope or injected context.

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
