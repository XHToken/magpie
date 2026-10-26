# API-Router Loomy host integration

Loomy runs an embedded OpenCode server. Its model menu, `opencode.json`,
`loomy-config.json` sidecar and Router's `apps.loomy` Profile are separate state.
A model appearing in the menu does not establish a usable Router binding.

## Runtime paths

- [`LoomyAdapter`](../../internal/routerhost/loomy.go) owns the `api_router`
  provider and selected Router model fields.
- [`hostAutoAPI`](../../internal/routerservice/host_auto.go) handles
  `GET`/`POST /api/hosts/auto`. `autoHostAppEdit` captures the Profile revision.
- [`validateAutoHostModels` and `hostConnectionMatches`](../../internal/routerservice/host_validation.go)
  validate targets and compare actual host files without upstream inference.
- [`Engine.ConnectAndCommit` and `Engine.RestoreAndCommit`](../../internal/routerhost/connection_commit.go)
  coordinate host files, the original recovery journal and Profile persistence.
- [`refreshLoomyRuntime`](../../internal/routerhost/loomy_runtime.go)
  synchronizes existing sidecar flags and invalidates the stale embedded server
  after successful host writes.
- [`loomy_title.mjs`](../../internal/routerhost/loomy_title.mjs) is Router's
  managed OpenCode extension; [`loomy_title.go`](../../internal/routerservice/loomy_title.go)
  owns the title request policy at the Router entry.

Automatic discovery uses `LOOMY_CONFIG_DIR`/`LOOMY_OPENCODE_DIR`, or exactly one
`%PUBLIC%/Loomy/<identity>/opencode` directory containing `opencode.json`.
Multiple candidates require an explicit directory. `POST /api/hosts/loomy`
remains a separate explicit-directory preview/apply/restore API with matching
preview digests; it does not edit the Profile automatically.

## Connect and synchronize

`POST /api/hosts/auto` validates the proposed stored-credential Profile and all
selected targets before writing files. Groups must exist, be enabled and have
active members. Direct cloud providers require saved credentials. Local IDs
must belong to the supplied service catalog. `FastChecker` independently checks
Router health; health alone does not establish model readiness.

The provider uses `npm: "@ai-sdk/openai-compatible"`,
`options.baseURL: "<origin>/app/loomy/v1"`,
`options.apiKey: "api-router-local-placeholder"` and `useSessionAuth: false`.
Application identity comes from the URL; cloud credentials stay in Router.
Every selected `group/<id>`, `<providerId>/<model>` or `local/<id>` is a literal
key in `provider.api_router.models` with `name` and `tool_call: true`.
Obsolete generated entries are retired; unrelated providers and user extensions
are preserved. The first selection becomes `model: "api_router/<target>"` and
the managed `small_model`. A later user-selected small model from another
provider is preserved. Existing sidecar custom-model flags and selection backup
are synchronized so Loomy retains the primary selection on startup.
An explicit reconnect repairs existing sidecar flags even when `opencode.json`
already matches the requested connection.

The host write transaction includes saving the captured Profile revision.
A failed save, stale revision or host write rolls back host and journal bytes
before runtime refresh. Interrupted transactions retain recovery metadata;
inspection detects any remaining Profile/file mismatch.

## Inspection and recovery

`GET /api/hosts/auto` is read-only. `connected` and `enabled` require a configured,
enabled Profile, a matching host model set, existing targets and matching
endpoint/provider fields. A host may select any model in its saved set.
JSONC and WorkBuddy's native array catalog are supported. Stale Router
references return `needsReconnect: true`; the Agents page shows a disabled
switch and "Reconnect required". Status never imports stale bindings or enables
a disabled Profile. An explicitly empty saved selection stays empty in the UI.

Disconnect restores host files and disables the Profile in the same rollback
operation. Omitting `models` retains the selection; `models: []` clears it.
Failed disconnects retain the previous UI selection and switch state.
Original snapshots reside at
`<Router state>/host-config/<host>-<root hash>.json`; reconnects preserve the
first baseline. A legacy baseline can itself contain Router entries, so a
restored journal alone does not establish a current connection.

`routerhost.SynchronizePorts` follows the actual service address for managed
hosts without inventing bindings or changing models. See
[host configuration](api-router-host-config.md).

## Title ownership and input

Connecting installs `api-router-title.mjs` in the selected configuration root
and adds its file URL to OpenCode's `plugin` list. This edits host configuration,
not Loomy's source, installed application or built-in providers. Adapter contract
v2 journals both files. Validated v1 records upgrade on reconnect while keeping
the first configuration backup. Disconnect removes only Router's added plugin
entry/file and preserves other plugins; an edited extension file is a conflict.

Loomy 0.9.39 / embedded OpenCode 1.14.22 has two title paths: OpenCode titles a
default session from the complete first user message, while Loomy's frontend
generates titles after completed turns 1, 4 and 8 using an internal
`__title_generation__` session and an explicitly selected managed model.
Hidden memory text is a normal text part in the former path. Its slower result
can overwrite Loomy's later title, even though the visible bubble omits memory.

The extension correlates the internal session's `chat-headers/<session>.json`
`ChatId` through Loomy's existing `../share/chat-session-map.json`. It reads the
actual source session's latest user model rather than assuming the global
default. Only an `api_router` source is redirected. Before its first main turn,
a recognized default title becomes the temporary `新任务`, preventing OpenCode
from starting a competing title writer. Existing/manual titles stay untouched.
Loomy retains its milestone and manual-title-lock behavior and writes the final
title. Missing/unreadable correlation never redirects another provider; the
original OpenCode title path remains available.

The extension supplies user text from the last eight turns and marks the title
request `X-Api-Router-Purpose: chat.title`. Router also recognizes the installed
native title-agent system signature for existing connections. Recognition is
restricted to Loomy's Chat Completions entry. Normal chat bodies are unchanged.
The ordinary authentication, complete input validation, model binding, routing
group policy, redaction, cancellation and attempt budget still apply.

Router removes the known memory-index block and system reminders from title
input, excludes assistant suggestions and bounds each user turn to 500 Unicode
characters. One shared prompt asks the selected model/group to describe the
actual interaction, with no invented task, and to return one short line in the
user's language. Every title, including greetings, invokes the model; there are
no fixed input-to-title mappings. Chinese titles are limited to 20 characters
and other titles to 50. Router buffers the model result, strips reasoning and
formatting, and emits complete JSON or SSE. Empty/failed output returns an error
so an existing title is retained. Raw memory and conversation text are not added
to request records.

## Verification

Run Go suites for `internal/routerhost`, `internal/routerconfig`,
`internal/routerrequest` and `internal/routerservice`, and
`internal/routerservice/tests/agents.test.cjs` with Chromium and WebKit.
Consistency tests reproduce orphaned menus, nonexistent groups and Profile-write
rollback. Host tests cover retirement, exact backups and failed disconnects.
Browser tests check failed clears and stale Profiles in all five UI languages.
Title tests cover hidden input removal, model-generated greetings, stream/error
boundaries, plugin scoping, manual titles, v1 upgrades and offline removal.
Run `node --test internal/routerhost/tests/loomy-title.test.mjs` as well.
