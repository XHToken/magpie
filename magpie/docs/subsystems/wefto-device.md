# 端云协同 device detection and model recommendation

[`internal/routerdevice`](../../internal/routerdevice/device.go) owns T3.1's
read-only device observations, hardware tier recommendation and device check.
It has no CLI/HTTP registration, startup hook, cache, polling, installer, network
client or process runner. Constructing `Native()` does no detection. The local-model
service calls detection for checks, installation, enablement and startup model
selection; this package itself owns no lifecycle.

## Product quantization advice

The product service uses [`AdviseModels`](../../internal/routerdevice/model_advice.go),
policy `spark26-64k-v1`. The older tier evaluator below remains available for
trusted diagnostic callers; it no longer chooses the product's quantization.
The service's detection response includes `advice` alongside the legacy
`recommendation` and `detection` summary.

`advice.state` is `recommended`, `not_recommended` (known weak/unsupported
hardware), or `deferred` (temporary pressure, missing information or slow
performance). A recommendation contains the exact catalog `modelID`, display
name, quantization, reason and `reuse` flag. `assessments` separates each model's
automatic suitability, explicit manual admission, safe resource issues and
per-volume installation budgets. A manual pass can coexist with a declined
automatic recommendation. Clients must use `advice`, not infer quantization or
manual admission from the legacy tier/check fields.

Automatic Q4 needs 16 GiB total / 8 GiB available RAM and either eight CPU
threads with CPU support, four threads plus a confirmed discrete accelerator
with 4 GiB left after reserve, or four threads with confirmed Apple Silicon
Metal. Q8 automatic advice needs either a confirmed discrete GPU with at least
12 GiB total / 9 GiB usable dedicated VRAM and 16/8 GiB host RAM plus four
threads, or confirmed Metal with 32/12 GiB unified RAM and eight threads. VRAM
reserve is at least 1 GiB or 15% of that single GPU's total. Shared memory and
multiple GPUs are never added. Pure CPU machines default to Q4; explicit Q8
CPU use needs 16 GiB total / 12 GiB available RAM and eight threads. Q4 manual admission
retains four threads and 8 GiB available RAM, without claiming automatic
suitability. Unknown required observations cannot authorize a download.
These are conservative 64K policy estimates, not measured minimums or quality
acceptance. The [approved design](../../../docs/local-model-recommendation-proposal.md)
records rationale and calibration limits.

`Adapter.FreeBytes` is usable only with matching backend/adapter evidence.
Native platform detection itself does not execute a model. The service may
augment observations by enumerating an already verified installed engine, with
strict unique native-adapter name matching on discrete devices. Missing engine
components remain unknown; detection never downloads them. The service samples
available RAM and both destination disks three times and retains the minimum, and exact-engine GPU probes
likewise retain only devices present in all three samples and their minimum
free memory. Known healthy running selections can be reused without charging
their resident memory again; slow measurements cannot authorize new automatic
hybrid use. No detection result unloads or deletes a model.

Native volume identity uses Windows volume GUIDs or Unix device IDs. Model and
runtime budgets sharing a volume are combined; separate volumes are checked
individually. Budgets include only the chosen model and installation plan plus
`max(2 GiB, 10%)` margin. Verified caches may reduce the plan; file presence does
not count as verification. Missing volume/free-space facts remain unknown.

## Consumer contract

`New(Provider)` injects a complete provider. [`Probes`](../../internal/routerdevice/probes.go)
replaces individual system, memory, adapter, disk and backend reads; it also
allows Linux fixtures to run on Windows/macOS without probing the runner.
`ProviderFunc` is the small integration seam for T3.2/T3.4 test doubles.

```go
// Paths come from dirs.Layout; no host config or credentials are inspected.
detector := routerdevice.Native()
report := detector.Detect(ctx, layout.Models, routerdevice.Requirements{
    Tier1_7BBytes: manifest.Small.InstallationBytes,
    Tier4BBytes:   manifest.Large.InstallationBytes,
})
// Display report.Recommendation and report.Detection separately.
// A later, explicit user action owns download, installation and enablement.
```

The example's manifest is T3.2-owned, not a manifest implementation in T3.1.
Disk requirements include model/runtime files and temporary installation space
on the measured volume. There is no guessed default: a zero/missing requirement
leaves the device check unknown. If a runtime will be placed on another volume,
T3.2 must check that destination separately before installation.

`Requirements.MinimumTier` optionally limits recommendations to models present
in the caller's catalog. With a `4B` floor, a primary `1.7B` recommendation is
promoted only when the hardware's existing `MaxTier` allows `4B`; otherwise it
becomes `none` with `catalog_requires_4b`. The promoted tier's disk reservation
and the normal OS/backend checks still apply. The local-model service uses
the quantization policy above instead; callers without a floor keep the
original two-tier behavior.

| State | Owner and meaning |
| --- | --- |
| `Report.Recommendation` | Primary recommended `Tier`, maximum allowed `MaxTier` (`Allows(target)`), and reason; does not imply installability |
| `Report.Detection` | Known minimum RAM, available physical RAM (`>= 2.5 GiB` when known), CPU logical threads (`>= 4` on CPU-only hosts when known), supported system/architecture, required host backend and sufficient destination disk; never model health |
| `ModelState.Installed` | Separate installer-owned `Fact[bool]`; verified files, not inferred by detection |
| `ModelState.Enabled` | Separate configuration/runtime-owned `Fact[bool]`; explicit user choice, not inferred by recommendation or installation |

`Fact.Value == nil` serializes as `null` and always means unknown. Measured disk
zero remains known zero. Invalid physical RAM zero is unknown. A per-probe error
discards values returned alongside it; a provider-level error or cancellation
discards the entire snapshot. This includes errors with an empty message.
Reports own their pointers/slices, so editing a report does not change later
observations. Providers used concurrently must themselves be concurrency-safe.

`Recommend` and `Evaluate` are pure functions. `Detect` calls the provider once
per explicit invocation, never reuses stale success, and accepts only an absolute
model destination. Native disk probes inspect the nearest existing directory
without creating it; denied reads, files and dangling symlinks remain errors.

## Legacy tier recommendation and check rules

Thresholds use exact bytes with `GiB = 1 << 30` and `HalfGiB = GiB / 2` for the
product's GB RAM/VRAM labels; there is no floating-point rounding before comparison.

1. Unknown/invalid RAM → `unknown`; known RAM below 8 GiB → `none` (`memory_below_8_gib`),
   including machines with large GPUs. The below-8 rule takes precedence.
2. When `AvailableMemoryBytes` is known and `< 2.5 GiB` → `none` (`available_memory_below_2_5_gib`).
3. When no GPU accelerator backend is confirmed available and known
   `System.LogicalCPUs < 4` → `none` (`cpu_threads_below_4`). A confirmed accelerator
   means `metal` on macOS or `cuda`/`vulkan` on other systems.
4. System RAM at least 24 GiB (`HighMemoryBytes`), available memory unknown or
   `>= 6.5 GiB`, and a confirmed accelerator or logical CPUs unknown or `>= 8` →
   `Tier: 4B, MaxTier: 4B` (`memory_at_least_24_gib`). This branch takes precedence
   over the discrete-VRAM rule.
5. Otherwise, a confirmed discrete adapter with at least 6 GiB VRAM (when
   `< 16 GiB` RAM, or when a matching `cuda`/`vulkan` backend is confirmed available),
   and available memory unknown or `>= 4.5 GiB` → `Tier: 4B, MaxTier: 4B`
   (`discrete_vram_at_least_6_gib`).
6. Remaining RAM at least 16 GiB (`MainstreamMemoryBytes`), including higher-RAM
   machines with limited available memory or CPU threads → recommends `Tier: 1.7B`:
   - If `(hasGPUAccelerator || LogicalCPUs unknown or >= 8)` and `(AvailableMemoryBytes unknown or >= 4.5 GiB)`:
     `Tier: 1.7B, MaxTier: 4B` (`memory_16_gib_prefer_1_7b_allow_4b`), allowing explicit 4B selection/enablement.
   - Otherwise: `Tier: 1.7B, MaxTier: 1.7B` (`memory_16_gib_limited_headroom`).
   Apple Silicon / unified memory follows these same RAM rules: confirmed Metal
   supplies accelerator evidence but does not itself upgrade 16 GiB RAM to a 4B recommendation.
7. Remaining known RAM from 8 GiB to below 16 GiB → `Tier: 1.7B, MaxTier: 1.7B` (`memory_at_least_8_gib`).

When `Evaluate` inspects destination disk space and `Recommendation.EffectiveMaxTier() == 4B`
while `Recommendation.Tier == 1.7B`, if disk free space covers `Tier1_7BBytes` but is below
`Tier4BBytes`, `Evaluate` caps `Report.Recommendation.MaxTier` to `1.7B` so `1.7B` remains
installable while `4B` is blocked.

Integrated/unified/shared memory, unknown adapter type and software/remote
adapters cannot supply discrete VRAM. Memory is not summed across GPUs. Unknown
GPU data cannot upgrade 8–16 GiB RAM.

An unknown/unsupported architecture, unknown/unavailable required backend,
unknown disk/manifest requirement, insufficient disk, known available memory
below 2.5 GiB, or CPU-only logical threads below 4 prevents `CheckPassed`.
Known failures dominate unknowns, and all check reason codes are retained.
The recommendation remains visible even when a device check cannot pass.

Windows x64, Linux x64 and macOS arm64/x64 accept a CPU host backend. A VRAM-based
4B recommendation additionally needs a confirmed CUDA/Vulkan backend with the
same nonempty `AdapterID` as a qualifying adapter; another GPU or CPU does not
prove that memory usable.
Native Windows/Linux optional accelerator backends currently remain unknown:
they are never guessed from DLL/library files. T3.2 can inject separately
validated backend evidence; T3.4 must still verify its exact runtime artifact.

No result grants permission to download, enables a local capability or changes
redaction. In particular, model unavailability while redaction is enabled must
still block at the later request gate; a recommendation is never a bypass.

## Native platform boundaries

Windows 10/11 x64 and macOS 14+ arm64/x64 are supported targets. Other
architectures return an unsupported check; missing Windows/macOS versions stay unknown.

| Platform | Native observations | Limits |
| --- | --- | --- |
| Windows | Installed physical RAM (`GetPhysicallyInstalledSystemMemory` / `GlobalMemoryStatusEx`), real-time available physical RAM (`GlobalMemoryStatusEx.AvailPhys`), native architecture, OS version, read-only CPU name and logical thread count, caller-available disk; DXGI adapters plus D3D12 architecture query | Only non-UMA, non-software/remote adapters count as discrete. Failed D3D12 classification stays unknown even if DXGI returns a large reserved segment. Optional CUDA/Vulkan evidence remains unknown. |
| macOS | `hw.memsize`, OS/CPU sysctl, process architecture, available filesystem blocks; Apple silicon unified GPU; current available physical RAM from Mach free + inactive pages | The detector and runtime headroom checks share `AvailableMemory`. Mach calls use purego in both cgo and no-cgo builds, without starting a process. Kernel page size is queried rather than assuming 4 KiB; speculative pages already belong to free pages and are not added again. Invalid counts, failed reads and cancellation stay unknown; known zero remains zero. Unified memory is never discrete VRAM. A cgo build queries `MTLCreateSystemDefaultDevice` without launching a runtime. A no-cgo build reports Metal unknown; CPU validation can still pass, but first-install automatic advice stays Q4 until Metal is verified by an installed engine. |
| Linux | Sysinfo RAM with checked units, real-time available RAM (`/proc/meminfo` `MemAvailable:` with sysinfo fallback), uname/architecture, logical CPUs and caller-available filesystem blocks | GPU/CPU model details not read are unknown. Injectable fixtures validate the contract separately. Ubuntu/kernel/driver and physical CPU/GPU acceptance remain later native validation. |

The product remains no-cgo. Legacy diagnostic callers may use
[`EvaluateWithCPUFallback`](../../internal/routerdevice/cpu_fallback.go) when
unknown GPU availability blocks an otherwise valid CPU installation. It evaluates
a separate CPU-only recommendation and preserves the original GPU observations
in the report. RAM, available-memory, CPU-thread, architecture and disk checks
still apply; GPU VRAM never becomes CPU RAM. The runtime later probes its exact
installed CUDA/Vulkan/Metal engine and validates real offload plus synthetic
inference, as described in the [runtime reference](wefto-runtime.md).
A no-cgo cross build proves compatibility only, not native Metal acceptance.
No Linux GPU release work is included.

The local-model page identifies `available_memory_unknown` as an unreadable
available-memory observation instead of showing only the generic device/disk
warning. Missing observations still block installation and load admission.

## Verification

Tests use `testenv.Main`, temporary HOME/USERPROFILE and data directories, and
fixture providers. They do not run native hardware queries, models or upstream
requests. `make test-router` includes this package. From `magpie/`:

```sh
gofmt -l internal/routerdevice
go test -race -count=20 ./internal/routerdevice
go vet ./internal/routerdevice
make test-router
```

Coverage includes 7.9/8/16 GiB RAM, 5.9/6 GiB VRAM, UMA/shared/unknown GPU,
multiple GPUs, failed/empty-error reads with residual values, unknown system,
architecture/backend, backend-to-adapter matching, disk below/at threshold,
missing requirements, cancellation, repeated/concurrent detection and detached
reports. Filesystem fixtures cover absent model directories and dangling
symlinks. A production import guard covers all Go platform files against
network/process/product lifecycle dependencies; the Metal bridge is also
reviewed for read-only behavior. Real hardware acceptance and cross compilation
are recorded separately in the T3.1 delivery record (historical development record; not distributed).
