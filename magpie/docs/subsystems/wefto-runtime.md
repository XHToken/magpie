# Local model installation and runtime

On macOS, downloader destination, runtime dependency and installation path checks accept the known system directory links `/var`, `/tmp` and `/etc` when they resolve to directories. Other filesystem links remain rejected; archive alias materialization and file integrity checks retain their existing constraints. Runtime selection uses the native architecture described in [device detection](wefto-device.md), including Apple Silicon under Rosetta.

`routermanifest`, `routerdownload`, `routerprocess` and `routerruntime` implement
T3.2–T3.8. The [management adapter](../../internal/routerservice/local_models.go)
owns HTTP/UI integration. Host adapters, provider credentials, routing decisions
and entity replacement algorithms are outside this subsystem.

## Source of truth and files

[`routermanifest.Fixed`](../../internal/routermanifest/manifest.go) is a detached,
compiled catalog: llama.cpp **b10828**, CPU Windows x64/Linux x64/macOS arm64/x64,
Windows x64 CUDA 12.4/Vulkan and macOS arm64 Metal metadata;
Spark-X2.6 **4B Q4_K_M / Q8_0**, exact revisions, lengths,
SHA256, fixed template, license and disk reservation. Runtime/weight metadata
was checked on 2026-10-17; accelerator release bytes were additionally hashed
on 2026-10-24. Native inference remains a platform-specific acceptance step.
The product enables automatic backend selection as described below.
The product serving context is 65536 tokens. Trusted diagnostic
settings also support 8K, 16K and 32K windows; the selected window is a serving
limit, not an assertion of validated 1M-context/tool compatibility. Archive expansion
uses each fixed artifact's expansion limit; the CUDA engine needs 640 MiB and
its CUDA dependency archive 576 MiB. Installation requirements include runtime
download, extraction, composition and dependency peaks plus model reservation.

The published models are **public execution validation weights**, not the
final judgment/redaction artifacts. Their metadata does not establish any of
the three purpose-specific acceptance results. Mirror metadata and fixed hashes
are recorded in catalog evidence (historical development record; not distributed).

Spark-X2.6-4B-Q4_K_M and Q8_0 metadata was checked on 2026-10-27 against the official
[GGUF repository](https://huggingface.co/XHToken/Spark-X2.6-4B-GGUF/tree/83b47fda018fd14f51aa63341f78266f628af5a5).
Their 3,543,441,312-byte Q4_K_M and 4,375,021,824-byte Q8_0 weights and shared
5,717-byte X2.6 chat template have
fixed revisions and SHA256 pins in `Fixed`; downloads use the same mirror and
resumable verification path. The official model retains the `spark2_5`
architecture and requires b10828 or later, so the existing runtime is reused.
Q4_K_M retains catalog ID `spark-x2.6-4B`, selector `local/spark-x2.6-4b`,
artifact identity and fingerprint. Q8_0 uses catalog ID `spark-x2.6-4B-Q8_0`
and selector `local/spark-x2.6-4b-q8_0`, with an independent weight artifact ID.
Their display names include the corresponding quantization.
[`Artifact.DownloadURLs`](../../internal/routermanifest/sources.go) defines
download priority separately from artifact identity. All catalog llama.cpp
b10828 archives (Windows CPU/CUDA/Vulkan, Linux CPU, macOS Intel/Apple Silicon)
and the CUDA 12.4 DLL archive try `gh-proxy.com` first, then GitHub. Existing
[`PlatformRuntime`](../../internal/routerruntime/storage.go) and
[`RuntimeCandidates`](../../internal/routerruntime/backend.go) still select
packages by OS, architecture and device hints; source fallback does not select
another platform or runtime version. Microsoft VC dependencies keep their
existing Microsoft source.

X2.6 Q4_K_M, Q8_0 and the shared template try HF-Mirror first, then ModelScope.
ModelScope's GGUF commit `8500d75a53f1e6592cfbb36fcb67c3e7380e6aab` and template
commit `1648980d7798c24041bedf58aff82615c562b6c6` have matching size/SHA256
metadata checked on 2026-10-27. The template was also downloaded and hashed;
both weight URLs and the runtime mirror variants passed nonzero Range probes.
Large weight/GPU archives were not fully downloaded for that source check.
The original artifact URLs remain serialized, preserving capability
fingerprints, download paths and extraction receipts. Every downloaded file,
from either source, must pass the original size and SHA256 checks.

Remote transfer or integrity failures advance to the next source under the
same download lock. User cancellation and local filesystem/disk failures stop
the job. An unreadable free-space probe is advisory and allows download,
extraction and assembly to proceed; a confirmed space shortfall still blocks,
and actual write/flush failures still stop the job. Source switches restart
that artifact from byte zero and clear its
validators; a later explicit retry first resumes the persisted source if it
still belongs to the compiled source list with matching size/hash pins.
Installation progress reports the actual source hostname. See the
[downloader contract](../../internal/routerdownload/README.md) for retry bounds.

Model labels use `Model.DisplayName`, independently of the shared `4B` hardware
tier. The service consumes per-quantization advice described in the
[device reference](wefto-device.md#product-quantization-advice), including
safe manual overrides and separate resource/volume checks. Explicit actions
accept unknown observations for explicit download/enable/load attempts while
retaining confirmed shortage and unsupported-runtime rejections. Automatic
recommendations still require known passing observations. Installation plans
credit only verified model/template/runtime caches and deduplicate shared
component artifacts. Each destination volume gets its own peak requirement
plus `max(2 GiB, 10%)` margin; Q4 never inherits Q8's larger weight reservation.
Complete installed models can be reused without requiring fresh-download space.
On macOS, explicit installation uses the device assessment's `download` state,
independent of inference memory/CPU headroom. A download-only request does not
enable the model. Enable/load and restored enabled intent use `manual` instead:
Q4 may be explicitly tried from 4 GiB available memory, while automatic advice
still uses 8 GiB. Q8 budgets and the 64K runtime context remain unchanged. These
policy estimates do not guarantee that a Mac can allocate the actual working
set; runtime loading, health and performance checks remain authoritative.
Selection, loading, CPU/GPU choice, queueing and capability gates are shared;
Native X2.6 inference
and purpose-specific quality still require actual model acceptance.

`config.json.runtime.enabled/selected/userDisabled` records user intent through
the existing configuration coordinator; unknown fields are preserved. Missing
fields mean disabled/no selection and not explicitly user-disabled. Invalid
values or unknown persisted selection fail closed. The two retired X2.5 IDs are
the explicit exception: `ConfigIntentStore.Load` clears their selection,
enablement and trial authorization in memory so service startup succeeds.
It preserves explicit user-disable intent, settings and the saved document;
the next explicit save persists the cleared or newly chosen model through the
existing coordinator. Old weights/caches and unrelated routing/provider/host
settings are retained. Retired IDs have no catalog, select or install path.
Explicit `Disable` persists
`userDisabled: true` while explicit `Enable` clears it. `routerruntime.New`
verifies selected files without starting a process or resuming downloads; when
the full Router service starts (`AutoLoad: true`) and `userDisabled` is false,
it asynchronously probes installed catalog models, selects/enables a compatible
installed model (preferring `selected`, then the hardware recommendation),
loads it, runs capability checks and enables experimental routing/execution
when those checks pass. Automatic enablement checks `userDisabled` under the
same operation lock as explicit enable/disable, so closing the model while
startup device checks are in flight cannot re-enable it. A selected installed
model remains enabled by default until the user disables it. Router shutdown
releases its process while retaining the saved choice for the next startup.

### Standard context and budgets

[`routerconfig.RuntimeSettings`](../../internal/routerconfig/runtime.go) owns the
complete `runtime.settings` object. The product service applies
`StandardRuntimeSettings()` through `ConfigIntentStore.StandardSettings` before
auto-loading or serving requests: all catalog models
use a 64K window and the same built-in time budgets. Users do not configure them.
Absent or former manual values resolve to the standard policy in memory. Startup
does not rewrite configuration or its backup, including when later host/port
synchronization fails. The next explicit intent save persists the policy through
the existing coordinator, preserving model selection, enable/disable intent,
trial authorization and unknown fields, and keeping the previous configuration
in the normal `.bak` file. Invalid or unreadable configuration remains an error.

| Setting | Standard product policy |
| --- | --- |
| Serving context | 65536 tokens (64K) |
| Judgment | 15 s queue, then 30 s inference |
| Execution | 30 s queue, then 300 s generation |
| Redaction | 30 s shared deadline across queueing and all extraction batches |
| Cold loading | 90 s |
| Whole business request | 600 s |
| Cloud fallback reserve | 60 s within the same request budget |

These are limits, not measured performance guarantees. In
particular, 64K requires loading, resource and latency checks on the actual
computer; model metadata alone does not prove that it fits available memory.

Trusted runtime callers and diagnostic fixtures retain support for the legacy
zero settings object (8K and one shared five-second judgment deadline), and for
explicit settings. `LongContextRuntimeSettings()` aliases the standard policy.
The product service always resolves to explicit standard settings.

[`Manager.UpdateSettings`](../../internal/routerruntime/settings.go), available
only to trusted in-process callers, stops local
admission, drains and reaps its one owned child, then saves through the existing
configuration coordinator. Any changed setting invalidates capability evidence;
changing context also changes the model fingerprint used in runtime bindings.
Existing weight/template downloads remain reusable. HTTP management no longer
accepts settings mutations. Normal startup and the model enable switch load the
model, run current capability checks and enable experimental use only when
those checks pass; a saved standard policy is not proof of a successful load.
Status exposes the resolved policy and effective targets; the latter are not
proof that a process is loaded. Requests bind a settings snapshot, and local
observation captures its identity under the same Manager lock. An older snapshot
cannot silently invoke or claim the identity of a replacement runtime.

Privacy specifications also bind the effective context and current model
fingerprint. A context change retires the previous pipeline cache epoch while
existing prepared response snapshots retain their own lifetime. A stale privacy
binding fails closed. This updates runtime identity only; the
[privacy input/output limits](wefto-redaction.md), experimental 30-second
policy and independent quality gates remain in force.

With explicit settings,
[`context.go`](../../internal/routerruntime/context.go) checks the final local
chat request inside the same scheduler slot, using the pinned server's
`/v1/chat/completions/input_tokens` endpoint. That endpoint uses the generation
template/tokenizer, including tools and template arguments. Positive caller
output limits are preserved and reserved. When no output limit is supplied, the
remaining context becomes the output ceiling, with at least 32 free positions;
the runtime does not silently shift away input history to continue generating.
Uncountable or oversized input has a distinct failure and can use the guarded
cloud fallback. Raw input and upstream error bodies are not written to ordinary
request records.

The explicit judgment path uses a versioned compact view, preserves the original
execution envelope, constrains output to local/cloud JSON with at most 32 output
tokens, and checks a 2048-token judgment input ceiling with that local tokenizer.
Fixed host instructions retain their source order in bounded summaries; tool
descriptions are bounded too. Duplicate history representations are removed
without dropping tool-call/result associations. A view that cannot
retain its detected restrictions or fit the token ceiling is reported as such
and falls back to cloud. This does not establish routing quality acceptance.

The business request deadline covers the entire operation. Local stages are
bounded by the remaining shared budget less the cloud reserve; cloud fallback
uses the original deadline and never starts a new request budget. A shorter
caller deadline or cancellation wins. A shorter management connection-check
deadline remains independent of the business-request preset. Output already
committed to a client still prevents fallback.

Downloads live under `Layout.Models/<artifact-id>/` and
`Layout.Runtime/<runtime-id>-archive/`; extraction is under
`Layout.Runtime/<runtime-id>/`. `.part` plus `.part.json` survive cancellation or
Router restart. Resume is an explicit install action and uses Range/If-Range;
an ignoring server restarts from byte zero. Resource changes, malformed ranges,
short/overlong responses and bad SHA are handled by the
[downloader](../../internal/routerdownload/README.md). OS file locks reject
duplicate writers across processes. Final files are flushed, exactly verified
and atomically renamed. Extraction materializes internal tar symlink chains as
ordinary file copies after all regular files and the gzip stream verify. Only
archived regular-file targets are accepted; absolute/escaping, cyclic, missing,
directory and directory-link targets are refused. Hard links, ZIP links,
special files, traversal and Windows aliases remain rejected; installed-tree
verification still refuses filesystem links. Alias copies have their own
size/SHA256 receipt entries and count toward the expansion limit before atomic
publication. The pinned macOS arm64 archive occupies 60,365,990 bytes after
materialization; Intel occupies 57,260,670 bytes, so its expansion budget is
64 MiB. Existing artifact URLs, archive hashes and cache paths are preserved.

Installation is proved by exact model/template hashes and reverified extraction
receipts, **never** by file existence, progress, config or `models/manifest.json`.
The latter is written after verification: schema 1 for runtimes without native
dependency metadata, schema 2 for the Windows app-local dependency contract below.

The management page renders `local_model_install_<stage>_<reason>` failures
from REST snapshots, streamed status and failed operation responses with the
actual progress step/source and a localized reason and retry action. Legacy
stage codes (`weights`/`corrupt`) remain readable. Integrity errors describe a
size/SHA256 mismatch without assuming network corruption: incorrect catalog
metadata can also reject a valid file. Template failures explain that retries
reuse verified weights and fetch the template again; they do not promise a
completion time. Retry still requires an explicit install action and every
cached artifact must pass the downloader's existing verification before reuse.

A cold load verifies again; corruption clears installed state and prevents launch. An explicit repair
quarantines a damaged extraction as `.invalid-<timestamp>` while no process uses
it, then publishes a fresh verified tree. Quarantined files are retained for
diagnosis and can consume disk until explicitly removed.

## Windows app-local dependencies

The Windows x64 CPU runtime additionally pins six release VC DLLs from an
official Microsoft VSIX in
[`windows_dependencies.go`](../../internal/routermanifest/windows_dependencies.go).
The fixed DLL version **14.50.35719.0** identifies this reviewed payload; it is
not a measured minimum version for b10828 or approval of other VC versions.
The VSIX's actual size is **3,313,422 bytes** and its SHA256 matches the official
catalog value; the catalog's size field instead says 3,325,839. The exact URLs,
per-file pins, source Authenticode observations, licensing conditions and this
size discrepancy are recorded in the
dependency design (historical development record; not distributed).
The VSIX OPC signature was not independently verified. Production verification
uses the compiled archive/file pins, PE architecture and fixed file versions;
it does not perform an online signature or revocation check at each launch.

Explicit installation downloads the VSIX through the same resumable downloader.
[`ExtractSelected`](../../internal/routerdownload/selected.go) checks all ZIP
headers and the total expansion bound, then writes only the six exact release
members into `Layout.Runtime/msvc-x64-14.50.35719.0/` with a v1 receipt. Debug
payloads remain inside the cached VSIX. A separate
[`Assembly`](../../internal/routerdownload/assembly.go) preserves the original
b10828 v1 extraction and creates
`Layout.Runtime/windows-amd64-cpu-b10828-msvc-x64-14.50.35719.0/`, placing the
selected DLLs directly beside the executable. Its v2 receipt must equal the
complete plan freshly derived from both verified source trees; all output
files are rehashed. Fixed per-file dependency pins are checked independently of
local receipts, and the completed model installation writes a schema-2 record.

[`ValidateDependencies`](../../internal/routerprocess/dependency.go) runs during
installation verification and immediately before native launch. It rejects
missing or altered files, wrong x64 DLL architecture/version, unsafe placement
and Windows reparse points/hard links. The owned child's working directory is
the verified executable directory; its loader environment uses system API paths
and excludes inherited loader overrides. No system VC install, System32 change,
global PATH change or diagnostic Python wrapper is required. App-local copies
remain the product's responsibility to update under the applicable Microsoft
redistribution license; system VC updates do not update these private copies.

An older installation lacking these files reports not installed and requires an
explicit install/repair. Startup, status reads and enable never download,
assemble or migrate it. Valid old GGUF/template/runtime caches are reused.
Explicit repair preserves damaged source/execution trees as `.invalid-*` before
replacement while no model uses them. Disable and close release
processes without deleting files; this change adds no uninstall endpoint or
automatic cache/quarantine cleanup.

Management status adds `dependencies` with state, fixed ID/version, error and
recovery. A dependency set reports `not_required`, `unknown`, `preparing`,
`ready`, `canceled` or a specific failure such as `missing`, `corrupt`,
`architecture`, `version`, `layout` or `failed`; recovery is
`install_or_repair` when needed. Management errors preserve stable
`local_model_dependency_*` codes. These fields remain separate from installation,
health and capability acceptance. The full dependency metadata participates in
the existing capability fingerprint. W4/W5's `Invoke`/diagnostic interfaces and
the owned-process lifecycle signatures are unchanged.
Selection and installation refresh preserve the fallback runtime's exact
dependency failure when no candidate verifies, including with automatic backend
selection; a missing dependency does not become an unknown status.

## Automatic CPU/GPU selection

[`backend.go`](../../internal/routerruntime/backend.go) owns the production
`AutoBackend` path. Windows tries CUDA, Vulkan, then CPU when system driver hints
are present; Apple Silicon tries Metal then CPU, while Intel macOS uses CPU.
Linux currently retains CPU. Hints decide optional installation and candidate
order; they never establish that acceleration works.

An explicit install/repair prepares CPU first and then hinted GPU packages.
Optional GPU download failure leaves verified CPU files usable and records a
fallback reason. Startup, status, enable and load never download missing files.
Existing CPU installations can use install/repair to add GPU components.
Apple Silicon CPU and Metal reuse the same pinned archive with different
runtime identities and launch flags.

The exact verified GPU engine runs a bounded `--list-devices` probe. Selection
uses one matching hardware device's free memory, excludes software renderers
and reserves at least 1 GiB or 15% of its reported memory. Memory is never summed
across GPUs. Explicit device, automatic layer fitting and memory reserve preserve
the serving context; fitting can produce partial offload. Health alone is
insufficient: nonzero offloaded layers, a matching device model buffer and a
fixed synthetic JSON inference are required. This proves execution availability,
not redaction quality.

Attempts share the cold-load deadline, with at most three model attempts.
Device probes have an eight-second bound; each GPU load gets at most 45 seconds
or half the remaining deadline. Failed children are stopped and reaped before
trying another backend. Failed GPU loads/inference enter a ten-minute cooldown;
explicit component repair clears it. Unexpected GPU exit faults the current
call and cools that backend; a later load may use CPU. Business redaction errors
do not trigger backend retries or bypass privacy gates.

`Status.backend` reports unloaded/loading, CPU, hybrid, accelerated or unavailable,
with the actual device, layer counts and safe fallback reason. The page shows
GPU acceleration only with a healthy owned process. Effective runtime, device,
actual layer counts and context bind capability fingerprints and privacy
identity. A change resets capability evidence and retires redaction caches;
free VRAM remains an observation and does not churn fingerprints.
Reloading the same runtime and execution plan retains capability sample
evidence, while every newly adopted process starts with response performance
unknown. A capability check that triggers a cold load binds its sample to the
runtime selected by that load, before executing the validator.

If no candidate passes installation verification, loading clears the installed
state so component/model repair remains available. Verification failure never
starts a process or downloads files; cancellation keeps its existing meaning.

CUDA composition adds separately pinned CUDA DLLs alongside the existing VC set.
Each DLL requires its own size/hash/PE architecture/version checks; CUDA file
versions legitimately differ. All source receipts and composition overlays are
verified. Increased DLL limits apply only to trusted CUDA plans; VC remains
strict. See the [process contract](../../internal/routerprocess/README.md).

## Lifetime and concurrency

[`Manager`](../../internal/routerruntime/manager.go) owns one selected model and
one process shared across applications and purposes. `NativeFactory` starts an
owned loopback child with `--parallel 1 --cache-reuse 256 --jinja`, the selected
serving context passed through `--ctx-size`,
verified model/template paths and CPU GPU-offload=0 when appropriate. JSON
`cache_prompt:true` enables the pinned server's prefix-cache path. This is a
configuration assertion only; actual KV-cache benefit is **not measured**.
See the [process contract](../../internal/routerprocess/README.md).

Intent, installation (`not_installed/downloading/installed/canceled/failed`),
runtime and capability samples are separate fields. Runtime transitions are:

- `unloaded → loading → ready → busy → idle`; shared cold starts use one worker.
- Verification/start/health/load timeout or crash → `fault`; children remain
  owned until termination is confirmed. Failed cleanup cannot spawn a successor.
- `idle/ready` remains loaded for the lifetime of the Router service. There is
  no product idle-unload timer; only an explicit disable, process fault or
  Router shutdown releases the process.
- Explicit disable first stops admission and invalidates old waiters, persists
  disabled intent, cancels installation/loading, rejects queued requests, drains
  active generation and releases the child. Later requests cannot reload it.
- Router close stops admission and drains before process cleanup, joins owned
  workers and retains durable enable intent for a future explicitly used service.
  Windows uses a kill-on-close Job assigned before the child resumes; abrupt
  parent death cannot leave a task-owned executing child. Other native platforms
  remain separately unverified.

There is one execution slot and FIFO waiting capacity **16**, shared by all
three purposes. During cold loading all waiting calls count against 16. A
single canceled cold waiter does not cancel another waiter's shared load.
Disabled/full/canceled/fault are distinct errors. Timers and `Now` are injectable.
An active child's crash returns `ErrFault`, not caller cancellation. Caller
cancellation/deadlines, explicit shutdown epochs and purpose deadlines retain
their own errors, so W4 can apply its pre-output fallback/cancel policy safely.
Trusted diagnostic callers may opt into `Options.IdleTimeout` and call `Sweep`;
zero disables idle eviction. The product never schedules `Sweep`. Lifecycle
transitions serialize so unload/disable/re-enable cannot overlap two live
processes. No blocking network/process operation holds the status mutex.

Cold loading has its own budget; the judgment and execution queue budgets begin
only after it succeeds. Legacy and explicit settings use the limits in
[Standard context and budgets](#standard-context-and-budgets). Redaction
retains its separate **30 s** shared queue/inference budget. A caller's shorter
deadline always bounds the operation. Disable's management drain budget is 120 s;
Router shutdown permits 5 s for HTTP and then bounded model cleanup. Expired drain
cancels active work, initiates owned-process termination and records a fault.

The explicit experimental-redaction startup option changes that Manager's
redaction budget to **30 s**, capped at 30 s. A shorter configured or
caller budget remains in effect. Its dedicated invoker also places cold loading
under the same request deadline; the experimental extractor owns the earlier
deadline shared by every batch. The ordinary redaction default is also 30 s.

An owned process requested to stop by Disable, Close or an explicitly configured
diagnostic Sweep is an
expected exit. Its monitor must not publish `process_exited` while Stop is
completing. This distinction is bound to the same process immediately before
Stop; merely draining work does not make a crash expected. Unexpected exit
still faults the runtime and cancels active work. A failed Stop or expired drain
retains its separate fault. If the child already exited before Stop, that fault
is recorded before ownership is cleared even when its monitor is delayed.
Successful lifecycle diagnostics additionally check
that the unloaded status has no residual error.

## Health, samples and W4/W5

GET `/health` must be HTTP 200 with parsed `status:"ok"`. Health does not pass a
capability. Judgment, execution and redaction independently begin `unknown`.
`CheckCapability(Check{Purpose,Suite,Run})` executes a registered sample validator
under the same scheduler; success requires a versioned suite and nonempty sample
evidence. The callback receives `(ctx, loopbackEndpoint)` and must use the given
context and validate actual output. It must not recursively call `Use/Generate`.
No acceptance callback or claimed evidence is accepted from HTTP clients.

Each result binds the full runtime/model/template fingerprint, suite and evidence.
Changing selection resets conclusions; restart resets them to unknown. `state`
is the sample result; `ready` is effective enable/installation/runtime gating.
An untested model never inherits results from a different revision.

The 端云协同 1.7B and 4B routing and execution checks use the same experimental
trial checks. They retain `state:"unknown"` and a separate `trialEvidence`;
they become usable only after an authenticated user calls
`/api/local-model/trial/enable` following a current `/check`. The resulting
`trialEnabled`/`trialReady` fields are independent of formal quality and of
the redaction trial. `/trial/disable` persists the explicit shutdown. Changing
the selected model or its bound runtime resets the evidence and makes a saved
trial intent inactive until the check is run again.

W4/W5 use `Manager.Invoke(ctx,purpose,chatJSON,consumeResponse)` after registering
their independent checks. `Invoke` enforces acceptance before loading and again
when taking the execution slot. `Generate` is the diagnostic/acceptance seam;
`Use` is the lower scheduler seam. `consumeResponse` must consume SSE/body inside
the callback and honor the response context; returning cancels the upstream and
releases the slot. No response or body may escape for later reading. Judgment
and redaction force `chat_template_kwargs.enable_thinking=false`; execution
preserves that setting and the complete messages/tools semantics. W4 owns prompt,
tool-argument compatibility, output validation and fallback. W5 owns entity
extraction, replacement/restoration and fail-closed cloud gating. Existing
`routerrequest` redaction-unavailable rejection is unchanged: disabling a runtime
does not clear any application's redaction intent or send original text to cloud.

`Options.ExperimentalRedaction` is a trusted startup choice with no management
HTTP setter. The runtime's zero value remains false; the product CLI enables
it by default unless explicitly disabled (see [service startup](wefto-service.md)).
Only that choice permits
`NewExperimentalRedactionInvoker(fingerprint,suite)`. The immutable handle
admits Redaction for either enabled fixed Spark-X2.6 Q4_K_M or Q8_0 selection
declared by `Model.SupportsRedactionTrial`, and its exact
current artifact fingerprint, with a complete `privacy-trial-v2:` suite plus
64 lowercase hexadecimal digits. `VerifyBinding` checks the experimental
extractor's exact fingerprint and suite on every request. Disabled, closing,
stopping, different-purpose/model/binding and incomplete-suite calls reject.

This handle is an explicit trial admission, not sample acceptance. It never
changes `Status.Capabilities`, calls `CheckCapability`, or fabricates evidence;
unknown capabilities remain unknown and ordinary `Require`/`Invoke` still
require their real acceptance. Its `Invoke` uses the same private generation
transport, single Manager/process, FIFO queue and execution slot. Trial binding
is checked again after obtaining the slot, and response consumption, caller
cancellation and disable use the same cleanup path. There is no recursive
Generate/Invoke call while holding the slot.

The W4 service adapter supplies a versioned `RuntimeJudge` over the Judgment
capability and a Manager-backed Execution invoker. The judge accepts only
`local` or `cloud`, and Manager adds `enable_thinking:false` for judgment. A
normal health check or ordinary answer never marks a capability available;
without a registered acceptance suite the runtime stays unknown and requests
use the guarded cloud path. The configured load, judgment and execution budgets
are independent of this capability admission; larger limits do not grant
acceptance or trial authorization.

## Management and validation

Trusted in-process callers can attach `WithGenerationDiagnostics` to one
Generate context and read its completed snapshot. It measures scheduler wait
plus load, request-to-headers, consumption and total wall time, with actual HTTP
status. These phases are not token estimates or isolated inference measurements.
HTTP failures retain a safe typed status; response bytes are retained only for
that explicit diagnostic context. Ordinary error formatting/JSON omits them.
No management HTTP option enables this seam, changes capability acceptance, or
chooses a model endpoint. Fixed synthetic privacy CLI evidence keeps raw
responses in a separate diagnostic directory.

All routes inherit exact local Host/peer/origin checks, management Bearer auth,
no-store and bounded input. Encoded aliases and arbitrary URLs/paths are rejected.

| Route under `/api/local-model` | Method / behavior |
| --- | --- |
| base | GET/HEAD: status and fixed catalog; no enable/install/load |
| `/events` | GET: authenticated SSE status snapshots on Manager changes; HEAD: headers only; no query parameters or lifecycle mutations |
| `/settings` | GET/HEAD: read-only status/catalog with saved and effective settings; mutation methods return 405 |
| `/detect` | POST: existing `routerdevice` report and recommendation |
| `/select` | POST `{model:<fixed-id>}`: disabled-only selection |
| `/enable` | POST: device checks bound to exact selection; persists enable only |
| `/install` | POST: recheck both destination volumes; start/resume install job |
| `/cancel` | POST: cancel and join installer; keep partials |
| `/load` | POST: explicit load under separate budget; no capability conclusion |
| `/health` | POST: process-only health; may be retried after health failure |
| `/check` | POST: binds server-owned judgment/execution suites and synthetic experience samples to the current X2.6 selection; no client-supplied evidence |
| `/trial/enable` | POST: after current checks and acceptable product performance, persist explicit experimental judgment/execution authorization |
| `/trial/disable` | POST: persist explicit experimental routing/execution shutdown |
| `/disable` | POST: persist disabled, drain and release; Router keeps listening |

The original-style **Local model** page is at `/?view=local-model`. It reads on
navigation/connect/manual refresh and polls transient install/load states only
while that page is open. All five languages and desktop/mobile layouts are
covered. Closing the page stops no Router or model process.

The page has no runtime settings section, context selector, numeric budgets or
save/reload action. The configured model's switch performs the existing
enable/load/check/trial sequence; startup follows the same capability gates.
The switch reflects saved enable intent independently of runtime health, so a
faulted or unloaded enabled model can still be explicitly turned off.
Resolving standard settings never enables or loads a disabled model.
Resolved settings remain in authenticated API status for diagnostics;
runtime health and current capability evidence must be checked independently.
Saved trial authorization remains the user's independent choice and is not
quality evidence.

Available physical memory measured around loading is an observation, not a
model-process peak-memory measurement.

The visible local-model page reads the full status/catalog on entry, explicit
refresh and action completion. During download, dependency preparation, loading
or unloading it opens one `/events` stream using the management Authorization
header. The stream sends an immediate status snapshot and coalesces change
notifications to at most two snapshots per second; a 15-second comment heartbeat
does not read status. Terminal transitions trigger one full read to update
installed candidates, then close the stream. Idle pages do not poll. Hidden or
departed pages abort observation; returning reads a fresh snapshot. Disconnects
retain the last state, show a connection error and retry with exponential delay
up to 30 seconds. Observation never owns or cancels installation. Slow readers
have a bounded write deadline and never block Manager notifications.

The local-model page manages model installation and runtime lifecycle. It has no
redaction diagnostic/acceptance panel and does not query `/api/redaction`.
Routing-group redaction controls and request redaction observations remain
separate; removing the panel does not change saved application/group intent or
cloud preparation requirements.

When the selected model has no verified installation (`not_installed`, `canceled`
or `failed`), the authenticated local-model page requests one device probe per
page session. This read-only hardware probe neither selects/enables a model nor
downloads or loads it; refreshing status does not repeat a failed probe. A manual
Detect device action retries it. The recommendation appears above the model
choices, which show the full names **Spark-X2.5-1.7B** and **Spark-X2.5-4B**.
Unknown or failed checks are distinguished from a passed recommendation. Both
tiers share the existing pinned download, resumable installation, dependency
repair and runtime lifecycle. Enablement and installation enforce the chosen
model's `advice.assessments[].manual` result. A declined automatic
recommendation does not forbid a safe explicit manual choice. Product native
starts also recheck the model's budget immediately before each backend attempt;
CPU fallback is re-evaluated using CPU resource requirements after a GPU failure.
The serving context is not reduced to make admission pass. Capability requests
use the selected model ID and its current fingerprint.

`/check` also runs two bounded warm synthetic SSE experience samples for the
selected, already loaded model. Each has approximately 1024 input tokens and
at most 96 output tokens; two samples with at least 32 reported output tokens
are needed. The median generation rate below 5 tokens/s or visible first-token
latency above 15 seconds produces `status.performance.state = slow`; insufficient
samples remain `unknown`. Acceptable samples produce `acceptable`. Metrics and
safe reported usage are recorded as diagnostic observations, never raw prompts
or replies. These short samples are not full-window performance or quality
acceptance. Measurement identity binds model/runtime/context, process PID and
lifecycle epoch; stale/replaced-process samples cannot authorize a successor.
Model/settings/backend identity changes invalidate the evidence.

Product experimental trial readiness and new trial authorization require current
acceptable performance as well as existing engineering checks. Slow or pending
performance preserves user intent and the loaded process but prevents new
automatic hybrid use. Trusted diagnostic managers retain their prior optional
policy. The page exposes a response-experience recheck through the existing
`/check` action. No measurement downloads a second model or calls a cloud provider.

Directly below the local-model actions, a persistent, politely announced result
card distinguishes a pending response check, an operation failure, slow or
unconfirmed performance, and passed response performance. Failures and warnings
show a descriptive heading, the reason and relevant recovery guidance before the
lifecycle notes. Available aggregate speed and first-token measurements are shown
with the existing 5 tokens/s and 15-second reference thresholds; unknown samples
never display a previous check's metrics. A retry replaces the previous operation
error. Passing response performance does not claim capability or quality acceptance.
Installation and runtime errors use the same visible card, preserving their
stage-specific explanations. Verification: `local-model-errors.test.cjs` covers
these outcomes and retries in five languages at desktop and mobile widths.

Core tests use controlled time, loopback download servers and the executable
fake; no real weights/network are needed. `product-regression.cjs` includes all
26 integrated product packages/commands and requires ten UI suites, including
Loomy, local-model, redaction, service-control, spark-models and tutorial. Run `node scripts/product-regression.cjs`; the CLI owns the common
heavy-test lock across Windows and WSL worktrees. The round-two integration also
uses the installed native Windows Go 1.26.8 for package tests and vet. Native
race lacks a C compiler; WSL race/vet/browser evidence is never Windows evidence.
The delivery (historical development record; not distributed) records exact results
and external model/acceptance gaps.

The internal [`real-model-check`](../../cmd/real-model-check/README.md) command
imports explicitly supplied local cache files through a loopback-only transport
while preserving the fixed artifact identities. It uses the same
`FileInstaller`, `Manager` and `NativeFactory`, including byte verification,
extraction receipts and installation records. Diagnostic `Generate` samples do
not register capability suites or make business `Invoke` available. Its
per-model data directory, raw responses and individual lifecycle assertions are
separate from the user's service data. The companion `model-fetch` acquisition
command keeps resumable prefixes and optionally caches bounded HTTP ranges;
only the production downloader's final fixed size/SHA256 check publishes the
complete file. These commands are internal acceptance tools, not management
HTTP local-path import endpoints or new production download sources.
Run these checks under the existing shared heavy lock. Evidence describes only
the exact native platform and samples actually run.

Round 3's service layer owns the same Manager for W4 judgment/execution and the
W5 production redactor. Selection changes reset capability fingerprints and
redaction cache epochs; existing prepared snapshots remain independent. The
Windows final2 EXE passed the 30-item native lifecycle/dependency diagnostic
for both fixed Spark tiers. This proves runtime/install behavior only; the
diagnostic deliberately leaves all three business capabilities unknown.

The local Manager also publishes the safe, nonblocking [`LocalCallObservation`](wefto-runtime-observation.md)
completion contract for Router business calls and explicit checks. It preserves actual nullable usage and measured
queue/load timing without recording prompts, responses, tools, entities, URLs or credentials; ledger persistence remains
owned by the statistics subsystem.
