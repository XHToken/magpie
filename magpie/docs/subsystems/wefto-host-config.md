# Wefto Host Configuration Transactions (WorkBuddy & Loomy)

## Purpose

Safely inspect, connect, synchronize, and restore external desktop agent host
configurations—specifically **WorkBuddy** (`~/.workbuddy/models.json` and
`~/.workbuddy/settings.json`) and **Loomy** (`~/.config/opencode/opencode.json`)—
with multi-model and routing-group (`group/<id>`) bindings, non-blocking fast
validation (`FastChecker`), automatic port follow-up, and backup recovery.

## Entry Points

- Core types & manager (`internal/routerhost/`):
  - `host.go`: `HostName`, `HostWorkBuddy`, `HostLoomy`, `Action`, `Options`,
    `FastChecker`, `ProbeChecker`, `Result`, `Manager`, `NewManager`
  - `workbuddy.go`: `inspectWorkBuddy`, `connectWorkBuddy`, `restoreWorkBuddy`,
    `workBuddyModelID`, `retireManagedWorkBuddyEntries`
  - `loomy.go`: `inspectLoomy`, `connectLoomy`, `restoreLoomy`,
    `retireManagedLoomyEntries`
  - `ports.go`, `ports_sync.go`, `ports_commit.go`: `SyncConnectedHostPorts`,
    `commitTransaction`, `loadRecoveryRecord`
- Service HTTP handlers (`internal/routerservice/hosts.go`):
  - `handleHosts` (`GET /api/hosts`, `POST /api/hosts/{host}/{action}`)
  - `handleHostsAuto` (`POST /api/hosts/auto`)

## Architecture

### Multi-Model & Routing-Group `Options` and `FastChecker`

`routerhost.Options` drives host connection and verification:

```go
type Options struct {
    AppID          string
    Model          string
    Models         []string
    ModelName      string
    ProviderName   string
    Vendor         string
    ContextLength  int
    MaxOutput      int
    SupportsImages bool
}
```

- `Options.Models []string` carries all selected model and routing-group IDs from
  `AppProfile.Models` (such as `group/<id>`, `<providerId>/<model>`, and
  `local/<engine>`). `resolvedModels()` deduplicates the list and guarantees the
  first item (`models[0]`) is used as the host's default primary model.
- **`FastChecker` Non-Blocking Validation**:
  - `routerhost.FastChecker` validates local configuration state without firing
    live upstream network probes.
  - During `POST /api/hosts/auto` (`action: "connect"`), `Service.fastCheckApp`
    satisfies `FastChecker` by verifying that the `AppProfile` exists and that
    every referenced routing group (`group/<id>`), cloud provider
    (`<providerId>/<model>`), or local model (`local/<id>`) is configured.
  - Full live upstream probing (`ProbeChecker`) remains available for explicit
    manual `verify` actions (`POST /api/hosts/{host}/verify`) without blocking
    the one-click agent onboarding flow.

### WorkBuddy Multi-Model Synchronization & Retirement

When connecting WorkBuddy (`connectWorkBuddy`):

1. **Multi-Model Catalogue (`models.json`)**:
   - Iterates over all items in `Options.Models` (`resolvedModels()`).
   - For each target `m`, generates a stable WorkBuddy entry ID via
     `workBuddyModelID(appID, m)` (`wefto-<appID>` for a single model or
     `wefto-<appID>-<slug>` when multiple models/groups are bound) with
     `id: entryID`, `model: m` (the wire model/group ID sent to Wefto, e.g.
     `group/claude-sonnet-4` or `openai/gpt-4.1`), `vendor`, `name`, and
     `apiKey: "wefto-local"`.
   - Calls `retireManagedWorkBuddyEntries` to remove any previously managed
     Wefto entries from `models.json` that are no longer in the current
     `Options.Models` selection, while preserving all user-owned non-Wefto
     models.
2. **Default Primary Model (`settings.json`)**:
   - Sets `codeMode.SelectedModel` and `chat.SelectedModel` in `settings.json`
     to the display name/ID of the **first** selected item (`models[0]`), and
     updates the endpoint URL to `<baseURL>/v1`.

### Loomy Multi-Model Synchronization & Retirement

When connecting Loomy (`connectLoomy`):

- Writes `provider.wefto` in `opencode.json` with `options.baseURL`
  (`<baseURL>/v1`), `options.headers["X-Wefto-App"] = appID`, and populates
  `provider.wefto.models` with every selected entry in `Options.Models`
  (including `group/<id>`, `<providerId>/<model>`, and `local/<id>`).
- Calls `retireManagedLoomyEntries` so stale model keys from previous connects
  are removed from `provider.wefto.models`.
- Sets top-level `model` and `small_model` to `wefto/<models[0]>`.

### Transactional Backups, Disconnect (`Restore`), and Port Sync

- Before mutating any host file, `commitTransaction` snapshots the pre-connection
  files into `%LOCALAPPDATA%/wefto/hosts/<host>/backups/` and records
  `recovery.json`.
- Calling `POST /api/hosts/auto` with `{"host": "<host>", "action": "disconnect"}`
  (or `POST /api/hosts/{host}/restore`) invokes `Manager.Restore`, which
  restores the backed-up host configuration files (or removes managed entries
  and restores previous default model fields if the file was created during
  connect).
- When Wefto switches loopback ports at startup, `SyncConnectedHostPorts`
  updates the endpoint port in already-connected WorkBuddy and Loomy files
  without altering their selected models or user settings.

## Wefto naming and existing recovery records

Product identity and aliases belong to [`internal/product`](../../internal/product/identity.go). New provider display names are `Wefto`; new Codex/Loomy provider keys are `wefto`. The known legacy `api_router` key, `API Router` label and local placeholder remain readable during an explicit upgrade. Codex and Loomy adapters publish version-2 contracts containing both field sets. Only their exact frozen version-1 contracts are accepted as legacy; arbitrary versions or fields are rejected. WorkBuddy retains its field contract and user-defined model names. Reconnect and offline recovery preserve the first original snapshot and later unrelated host edits. Host transactions lock both `.wefto-host.lock` and `.api-router-host.lock` so an older writer cannot run concurrently.
