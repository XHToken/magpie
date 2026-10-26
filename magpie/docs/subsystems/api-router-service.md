# API-Router Service Lifecycle, Loopback Auth, and Management Console

## Purpose

Host the standalone local HTTP service (`cmd/api-router`), loopback listener,
management token authentication, runtime settings endpoints, routing-group and
host-connection endpoints, and the embedded single-page management console.

## Entry Points

- Binary entry: `cmd/api-router/main.go`
- Service lifecycle: `internal/routerservice/service.go` (`Options`, `Info`,
  `Running`, `Start`, `Run`, `LockDir`)
- Listener & port binding: `internal/routerservice/ports.go` (`listenAvailable`,
  `startupPortTargets`, `persistStartupAddress`)
- HTTP routes & middleware: `internal/routerservice/http.go` (`handler`,
  `handlerWithModelOptions`, `requireManagement`, `managementSession`)
- Routing groups API: `internal/routerservice/groups.go`
  (`routingGroupsAPI`, `routingGroupSwitchAPI`, `effectiveGroups`)
- Host auto-connection API: `internal/routerservice/host_auto.go`
  (`hostAutoAPI`, `inspectAutoHosts`, `syncAutoHostStore`, `resolveAutoHostRoot`)
  and `internal/routerservice/loomy.go` (`loomyHostAPI`, `autoHostConfigRoot`)
- Embedded web console (`internal/routerservice/assets/`):
  - `index.html`, `app.js`, `provider.js`, `profiles.js`,
    `routing-groups.js`, `routing-groups-editor.js`, `observations.js`,
    `models.js`, `redaction.js`, `usage.js`, `spark.js`, `tutorial.js`

## Architecture

### Startup Sequence and Automatic Loopback Port Selection

`Start` initializes state and binds the loopback listener without killing
unrelated processes:

1. Ensures `dirs.Layout` directories exist and initializes `config.json` and
   `credentials.json`.
2. Loads runtime settings, redaction policy, local engine manager, usage ledger,
   and observation collector.
3. Acquires `<state>/router.lock` (`owner.json` + `management-token`). If an
   identical instance is already live, startup fails with a running lock error;
   stale locks whose recorded PID has exited or been reused are archived.
4. Binds the configured loopback port (`127.0.0.1:<port>`, default `13145`). If
   occupied by an unrelated listener, `listenAvailable` automatically binds the
   next available loopback port, `routerhost.SynchronizePorts` updates already
   connected Loomy and WorkBuddy configurations, and `persistStartupAddress`
   persists the newly bound address to `config.json`.

### Loopback Isolation and Admin Token Authentication

- All requests must originate from a loopback remote address and match the bound
  `Host` and same-origin checks (`http.go`).
- Management API routes (`/api/*`) require either the management session cookie
  or `Authorization: Bearer <token>` matching the per-run random token stored in
  `<state>/router.lock/management-token` (`requireManagement`).
- Gateway data-plane routes (`/app/{appId}/v1/chat/completions`,
  `/app/{appId}/v1/responses`, `/app/{appId}/v1/messages`,
  `/app/{appId}/v1/messages/count_tokens`, `/app/{appId}/v1/models`) accept
  loopback agent traffic governed by the target `AppProfile`'s `CredentialMode`
  and reject management bearer tokens on model routes.

### Management HTTP API Surface

- `GET /healthz`, `GET /api/status`: service identity, bound address, version,
  PID, and runtime status.
- `GET/PUT /api/runtime/settings`: read or update `RuntimeSettings`.
- `GET/POST /api/cloud-providers`, `DELETE /api/cloud-providers/{id}`,
  `POST /api/cloud-providers/{id}/check`, `POST /api/cloud-providers/{id}/models`:
  cloud provider CRUD, model discovery, and `localModels` listing.
- `GET/POST /api/routing-groups`, `PUT/DELETE /api/routing-groups/{id}`,
  `POST /api/routing-groups/switch`: list persisted and auto-merged routing
  groups (`EffectiveGroups`), create/update/delete or hide groups, and switch
  the active target for `manual` groups.
- `GET/POST /api/apps`, `DELETE /api/apps/{id}`, `POST /api/apps/{id}/check`:
  manage `AppProfile` records (`Enabled`, `Models []string`).
- `GET /api/hosts`, `POST /api/hosts/{host}/{action}`, `POST /api/hosts/auto`:
  inspect host status (`loomy`, `workbuddy`), run one-click multi-model /
  routing-group connection (`action: "connect"`) using `FastChecker`
  non-blocking validation, or restore host backups (`action: "disconnect"`).
- `GET /api/local-models`, `POST /api/local-models/{engine}/{action}`: local
  engine lifecycle (`llama_cpp`, `mlx`).
- `GET/PUT /api/redaction`, `POST /api/redaction/preview`: privacy redaction
  configuration and live preview.
- `GET /api/usage/summary`, `GET /api/observations`, `GET /api/observations/{id}`:
  token/cost rollups and per-request diagnostic ledger records.

### Management Console Views (`assets/`)

The single-page console (`index.html`) organizes workflows into six views:

- `#view-agents` (`profiles.js`): two-row agent onboarding (`loomy`,
  `workbuddy`) with `.field.solo.ag-menu` + `.pop.am-pop` multi-select model &
  routing-group dropdown and `.lib-switch.ag-conn` connection switch.
- `#view-routing` (`routing-groups.js`, `routing-groups-editor.js`): dedicated
  to routing group cards (persisted and `Auto` merged groups), strategy badges
  (`smart_split`, `ordered`, `weighted`, `manual`), member toggles, and the
  routing-group editor drawer (`#groupDrawer`).
- `#view-providers` (`provider.js`): cloud provider cards, preset/discovery
  model selector, and the bottom `#localProvidersSection` read-only configured
  local models section.
- `#view-local` (`models.js`): on-device engine (`llama_cpp`, `mlx`) download,
  start/stop, and health verification.
- `#view-redaction` (`redaction.js`): built-in PII detectors, custom sensitive
  words, and redaction preview.
- `#view-usage` (`usage.js`, `observations.js`): token/cost usage charts and
  breakdowns at the top, with the request observation ledger and diagnostic
  drawer (`#ledgerPane`, `#obsBody`, `#obsDrawer`) integrated at the bottom.

The usage guide restores focus to its invoking header, resume or settings
control after closing, for both pointer and keyboard activation; automatic
opening retains the previous focus or uses the header entry as a fallback.
