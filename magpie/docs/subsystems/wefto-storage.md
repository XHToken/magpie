# 端云协同 storage and runtime state

[`dirs.Layout`](../../internal/dirs/dirs.go) resolves the product paths once at startup. [`product`](../../internal/product/identity.go) owns new names and explicit legacy identifiers. Resolution is read-only; `Layout.Ensure` creates writable directories with private modes. Product storage is separate from Magpie settings, accounts and plugin migrations.

## Layout and rename compatibility

| Platform | Configuration | Durable data | Cache | State and logs |
| --- | --- | --- | --- | --- |
| Windows | `%APPDATA%/wefto` | `%LOCALAPPDATA%/wefto` | data/cache | data/state, data/logs |
| macOS | `~/Library/Application Support/wefto` | same as configuration | `~/Library/Caches/wefto` | data/state, `~/Library/Logs/wefto` |
| Linux | `$XDG_CONFIG_HOME/wefto` or `~/.config/wefto` | `$XDG_DATA_HOME/wefto` or `~/.local/share/wefto` | `$XDG_CACHE_HOME/wefto` or `~/.cache/wefto` | `$XDG_STATE_HOME/wefto` or `~/.local/state/wefto`; logs beneath state |

Models and inference runtimes live in `data/models` and `data/runtime`. Configuration and credentials are `config/config.json` and `config/credentials.json`. `state/router.lock` is the single-instance directory; `instance.json` and the private `management-token` live inside. `Layout.Install` is the executable directory, separate from writable state. Environment directory overrides must be absolute.

macOS also honors absolute `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME` and `XDG_STATE_HOME` overrides. With no override it retains the native Library paths above. Passing the resolved roots back through a service start command preserves that layout, including its state and log locations.

`Resolve` checks the equivalent `api-router` layout using `Lstat`. If only legacy storage exists, it returns that entire layout in place, including models, cache, state and logs. It never creates new configuration while retaining credentials or backups elsewhere. If both layouts exist, or an observed root is unreadable, a link or a non-directory, resolution fails for explicit reconciliation. No automatic copying, moving, merging or deletion occurs. See the [upgrade guide](../wefto-upgrade.md).

## Configuration and secrets

[`routerredact.ResultStore`](../../internal/routerredact/result_store.go) now stores
per-user-message recognition evidence in `<state>/redaction-results/`, separately
from request records and observations. It uses the resolved complete legacy or
current `dirs.Layout`; it never selects another configuration root. Windows,
Linux and macOS implement system key protection; other operating systems return
`redaction_store_platform_unsupported`.

The new directory has a protected DACL granting only the current user and SYSTEM
access; elevated Windows owners may be Administrators. Directory/file DACLs are
checked, and all path ancestors reject reparse points. Current-user DPAPI with
UI forbidden wraps a persistent random master key in `keys.dpapi`.

On Linux, [`result_store_linux.go`](../../internal/routerredact/result_store_linux.go)
uses the session D-Bus Secret Service's unlocked default collection (GNOME
Keyring or another compatible service). On macOS,
[`result_store_darwin.go`](../../internal/routerredact/result_store_darwin.go)
calls Security.framework/CoreFoundation directly through purego, including in
CGO-disabled portable builds, and uses the user's unlocked default Keychain.
Neither backend sends secret bytes in process arguments. The vault item is a
random per-store account under `org.xhtoken.wefto.redaction-results.v1`.
`keys.vault` stores only the backend, account and digest; the complete key bundle
remains in the OS vault. A read-back must succeed before initial DB creation.
Missing/locked/unavailable vaults return `redaction_store_key_unavailable`; there
is no plaintext fallback or automatic overwrite of an existing vault item.
Unlock the keyring/Keychain in the user session, then restart the service to
retry. Headless Linux requires a provisioned, unlocked Secret Service session.

Unix directories/files require current-user ownership and 0700/0600 modes;
multiply linked files and unsafe path components are rejected. Linux ACLs are
bounded by the required zero group permission mask. macOS clears inherited ACLs
on a newly created store and rejects extended ACLs on existing store files.
Only Apple's root-owned `/var`, `/tmp`, `/etc` aliases to `/private` are allowed;
application symlinks remain rejected. Nonblocking `flock` excludes a second writer.

HMAC index and AES-256-GCM data keys are independently derived. Query keys contain no original
text. Encrypted payloads contain entity coordinates/category/necessary originals,
block coverage, complete state, content/material/scope/revision metadata; AAD binds
the store, schema, key version and result key. Complete zero-hit records are also
authenticated. Full user messages, assistant/tool history and restoration tables
are not saved in this store.

`owner.lock` excludes a second writer. SQLite uses DELETE journal mode,
`synchronous=EXTRA`, bounded busy timeout, and a 256 MiB page cap, with no TTL or
automatic pruning. Inference runs outside database transactions. A committed
complete result is required before dispatch; failures never fall back to an
in-memory-only or plaintext result. The key bundle is synced before initial DB
creation, and the DB identity is authenticated against its storeId. Missing keys,
partial initialization, corrupt payloads, unknown schema, unexpected files and
permissions errors preserve the directory and block redaction. Automatic partial
initialization recovery, key rotation, backups, and per-record quarantine/recovery
commands are still pending; do not delete or recreate the directory to repair it.
An empty private directory containing at most `owner.lock` can retry initial vault
connection after it becomes available; existing key/database files are never
reinitialized by that retry.

Application recognition scope is derived from the service-resolved Profile's
`authorizationGeneration` (legacy absent value means 1). New registrations,
credential-mode changes and application revocation advance it; provider/model
changes do not. The broader [history plan](../redaction-history-protection-plan-20261030.md)
and [implementation record](../redaction-history-implementation-20261030.md) describe
remaining session/revision and recovery requirements.

Verification: `go test -race ./internal/routerredact ./internal/routerservice`
uses synthetic protected keys for ordinary history/service fixtures and never
reads the login vault. Native Linux acceptance runs
`sh scripts/test-redaction-secret-service.sh` with `GO_EXE` and Go caches pinned;
it requires `dbus-run-session` and `gnome-keyring-daemon`, creates a private bus
and disposable password-protected keyring, and checks actual HTTP egress,
response restoration, cross-process reopening and locked-vault preservation.
On macOS, `go test -run TestMacNativeKeychainRoundTripAndLock ./internal/routerredact`
uses a temporary native keychain, never the login keychain. Cross compilation
does not replace running this test on a Mac.

[`routerconfig.Store`](../../internal/routerconfig/config.go) owns schema-1 configuration with a nonempty instance identity and listen, apps, providers, redaction and runtime objects. Unknown JSON fields round-trip. Updates retain previous `.bak` files and write atomically through [`steady`](../../internal/steady); a private configuration lock serializes transactions.

Provider records contain credential references and previews; API keys remain in `credentials.json`. Application Profiles hold ordered model/group references, with the first synchronized to legacy primary-model fields. Provider deletion rejects references from saved applications or groups. Model/runtime artifacts and request ledger records remain in their existing owners' paths; renaming does not rewrite their contents.

## Shared file replacement

Windows [`steady` readers and publishers](../../internal/steady/guard_windows.go) coordinate each metadata lookup, content read and rename through a named kernel mutex for the normalized absolute pathname. Relative, case-varied and extended Win32 spellings share that name; the hash keeps the pathname out of the kernel object name. The mutex spans processes and Windows sessions on this computer, uses the creator's default access control, and creates no files during a read. Each acquisition queues for at most the existing 10 ms pause so continuous publishers cannot win every immediate poll. A busy mutex returns a sharing error to the existing bounded retry counts; cancellation is checked between attempts. Ownership stays on one OS thread through release, and an abandoned mutex is acquired normally after its owner exits. Different pathnames remain independent. Files reached through distinct link or short-name aliases, and older or external writers that do not use this protocol, retain the bounded I/O retries rather than this coordination guarantee.

[`steady.Rename` and `WriteFileContext`](../../internal/steady/steady.go) publish staged state files beside their destination. On Windows these sibling replacements use [`os.Root.Rename`](../../internal/steady/rename_windows.go), which requests handle-based POSIX replacement and owns the filesystem fallback. On supporting filesystems such as NTFS, a reader that permits deletion can finish reading the previous generation while new readers open the complete replacement. A reader that denies replacement is still waited out by the existing bounded retry. Moves between different directory paths retain `os.Rename` behavior; the staged file is flushed before publication and a failed write removes its staging file.

[`ReadFile` / `ReadThere`](../../internal/steady/read_windows.go) open Windows files with read, write and delete sharing, so reads of the previous generation cooperate with that replacement. Each read owns and closes one handle; ordinary Win32 resolution still follows links outside the parent, and long absolute and relative paths retain extended-path support. Sharing flags do not grant read access: a handle that denies readers still produces a read-sharing error after the bounded retry.

[`Stat` / `StatOnce`](../../internal/steady/stat_windows.go) query metadata from a handle with attribute-only access and the same replacement sharing. A read-data restriction does not prevent metadata lookup. `filememo` uses `StatOnce` for its known-absent fast path, so that path retains a single lookup without reverting to the path-attribute API. A newly created file is detected after a cached absence; true deletion still returns `fs.ErrNotExist`.

[`filememo.Read`](../../internal/filememo/filememo.go) continues to report genuine deletion as `fs.ErrNotExist` and other read failures as errors. The replacement fix does not return a cached account list on failure or parse a missing file as empty. The existing bounded read retries remain for replacement by older or external writers.

## Service and host recovery

[`routercontrol`](../../internal/routercontrol/control.go) verifies the recorded executable path, process stamp, instance identity and authenticated service before control operations. [`routerservice`](../../internal/routerservice/service.go) acquires the instance lock, binds a loopback listener and persists the actual address. Unrelated listeners remain running; another available port is selected and established host connections synchronize transactionally.

Recognized records are archived only after their process is proven exited or its PID reused. Live and unknown locks remain intact. Legacy storage keeps the old Windows autostart entry name so an existing entry can be inspected; entries belonging to another executable are never adopted or overwritten.

Host recovery records retain the original root, first snapshot, checksum and declared adapter contract. The Codex version-2 adapter reads its frozen v1 contract. The Loomy version-3 adapter reads frozen API-router v1/v2 and 端云协同 v2 contracts without losing their first snapshot; arbitrary versions or fields are rejected. Host transactions acquire both new and legacy writer locks. Details belong to [host configuration](wefto-host-config.md) and [Loomy](wefto-loomy.md).

Verification: `go test ./internal/dirs ./internal/routerconfig ./internal/routerhost ./internal/routercontrol ./internal/steady ./internal/filememo`, then the [complete product gate](../product-regression.md). Rename tests cover all platform layouts, retained credentials/models/recovery sentinels, dual-layout conflicts, unknown roots and legacy host restore/reconnect. Native Windows replacement tests keep an old reader open through both publication paths, reject missing sources and read-only destinations without losing the previous generation, and exercise concurrent replacement under `-race`.
