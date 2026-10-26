# Wefto host configuration transactions

The [`routerhost`](../../internal/routerhost/types.go) package plans, journals
and restores explicit host changes. WorkBuddy and Loomy use
`POST /api/hosts/auto` for automatic connections. The explicit Loomy
preview/apply API and offline `host restore-preview`/`host restore` CLI remain
separate. See [Loomy integration](wefto-loomy.md) and
[pinned protocol contracts](wefto-host-contracts.md).

## Options and files

`routerhost.Options` contains `Host`, `ConfigRoot`, `RouterURL`, `Model` and
`Models`. `EffectiveModels` deduplicates selections; `NormalizeOptions` sets the
primary to the first selection. Targets are `group/<id>`, `<providerId>/<model>`
or `local/<id>`. The service validates the proposed Profile revision and every
selected target before host writes, including group existence, enabled state,
active members and direct provider credentials. These checks run no inference.
`FastChecker` independently checks health; it does not prove model readiness.

`WorkBuddyAdapter` manages `models.json` (native root array or CLI object with
`models`) and JSONC `settings.json`. Each raw target ID uses
`<origin>/app/workbuddy/v1`, the public placeholder, `supportsToolCall: true`
and `useCustomProtocol: false`. `settings.model` is `custom-local:<primary>`.
Stale generated entries are retired; user entries and extensions survive.

`LoomyAdapter` manages JSONC `opencode.json`: new connections use the `wefto`
provider, while established `api_router` connections remain recognized. It uses
`<origin>/app/loomy/v1`, the public placeholder and `useSessionAuth: false`.
It publishes all selected IDs and selects `<provider>/<primary>`. Sidecar state,
small-model preservation and runtime refresh are documented in
[Loomy integration](wefto-loomy.md).
Contract v3 also manages `api-router-title.mjs` and its own `plugin` list entry
in the same transaction. The filename is retained for recovery ownership.
The exact released API-router v1, API-router v2 with title support, and Wefto v2
with only `opencode.json` upgrade without replacing their first baseline.
Other contract versions or field declarations remain conflicts.

## Transactions and status

[`ConnectAndCommit` and `RestoreAndCommit`](../../internal/routerhost/connection_commit.go)
include host files, the original recovery journal and Profile persistence in
[`commitHostGroups`](../../internal/routerhost/ports_commit.go). Failed host
writes, Profile saves or stale Profile revisions roll back host and journal
bytes. Runtime refresh occurs after commit. Interrupted or incomplete rollback
retains recovery metadata; inspection reports any Profile/file mismatch.

First pre-connection snapshots are retained in
`<Router state>/host-config/<host>-<root hash>.json`. Switching models retires
obsolete generated catalog entries while retaining the original baseline.
An explicit reconnect also accepts host files that exactly match that validated
baseline after the host restored them independently; other conflicts still stop
the operation.
Offline restore works while Router is stopped. Disconnect retains selections
unless the caller explicitly supplies `models: []`.

Status requires agreement between the enabled Profile and host files, including
catalog membership and the service endpoint. Stale Router references produce
`needsReconnect: true`; merely selecting them does not enable a connection.
Status reads never rewrite host files.

`routerhost.SynchronizePorts` follows the actual loopback address for already
managed hosts. WorkBuddy stores the endpoint on each model: all entries with
the same managed origin and placeholder follow the new address together;
entries belonging to another origin or key remain untouched. Its transaction
and recovery preserve model/provider settings and never kill an unrelated port
owner.

## Verification

Run `internal/routerhost` and `internal/routerservice` Go suites and the
Chromium/WebKit Agents and Loomy host browser suites. Isolated host roots cover
model changes, first-baseline preservation, Profile-write rollback, stale
configuration and offline restoration.
