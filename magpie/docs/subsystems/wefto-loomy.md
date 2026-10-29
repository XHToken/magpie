# 端云协同 Loomy host integration

Loomy runs an embedded OpenCode server. Its model menu, `opencode.json`,
`loomy-config.json` sidecar and Router's `apps.loomy` Profile are separate state.
A model appearing in the menu does not establish a usable Router binding.

## Runtime paths

- [`LoomyAdapter`](../../internal/routerhost/loomy.go) owns the `wefto` provider,
  recognizes existing `api_router` connections, and owns selected Router model fields.
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

Automatic discovery uses `LOOMY_CONFIG_DIR`/`LOOMY_OPENCODE_DIR` first. Otherwise,
macOS and Linux use `~/.config/loomy-opencode`, matching Loomy's
`electron/config-path.js:getOpenCodeDir` (verified against client revision
`63b5ff24a`). This path is independent of `XDG_CONFIG_HOME` and Electron's
`userData` directory. Windows uses exactly one
`%PUBLIC%/Loomy/<identity>/opencode` directory. Discovery requires an existing
`opencode.json`; it never creates a missing installation or adopts standalone
OpenCode's `~/.config/opencode`. Multiple Windows candidates require an explicit
directory. The same discovery supplies connect, status, disconnect and startup
port synchronization. `POST /api/hosts/loomy`
remains a separate explicit-directory preview/apply/restore API with matching
preview digests; it does not edit the Profile automatically.

## Connect and synchronize

`POST /api/hosts/auto` validates the proposed stored-credential Profile and all
selected targets before writing files. Groups must exist, be enabled and have
active members. Direct cloud providers require saved credentials. Local IDs
must belong to the supplied service catalog. `FastChecker` independently checks
Router health; health alone does not establish model readiness.

The provider display name is `端云协同`; its `api_router` routing identity is stable.
The provider uses `npm: "@ai-sdk/openai-compatible"`,
`options.baseURL: "<origin>/app/loomy/v1"`,
`options.apiKey: "wefto-local-placeholder"` and `useSessionAuth: false`.
Application identity comes from the URL; cloud credentials stay in Router.
Every selected `group/<id>`, `<providerId>/<model>` or `local/<id>` is a literal
key in `provider.wefto.models` with `name` and `tool_call: true`.
The model key and `wefto/<target>` selector remain routing identities;
`name` is a separate display label resolved from Router's saved configuration by
[`hostModelNames`](../../internal/routerservice/host_model_names.go). Cloud
labels are `<model> · <provider name>`, groups use their configured group names
(including effective automatic groups), and fixed local models use their
catalog names, such as `Spark-X2.5-1.7B`. Unknown catalog entries fall back to
the model portion of the selector; they do not expose a provider UUID as a label.
The explicit-directory preview/apply API uses the same label resolver for newly
generated entries. Its digest includes the resolved labels, so catalog changes
between preview and apply require another preview.

Reconnecting through `/api/hosts/auto` upgrades legacy ID labels and refreshes
previously generated labels after provider/group renames. The host catalog and
recovery journal change in the same Profile transaction via
[`synchronizeLoomyModelNames`](../../internal/routerhost/loomy_names.go), retaining
the first pre-connection snapshot. Existing user labels and later user aliases
are preserved. Status reads never migrate names; an already running Loomy may
need to be reopened to reload its catalog after reconnecting.

Obsolete generated entries are retired; unrelated providers and user extensions
are preserved. The first selection becomes `model: "<provider>/<target>"` and
the managed `small_model`. A later user-selected small model from another
provider is preserved. Existing sidecar custom-model flags and selection backup
are synchronized so Loomy retains the primary selection on startup.
An explicit reconnect repairs existing sidecar flags even when `opencode.json`
already matches the requested connection.
After committed configuration changes, runtime invalidation recognizes the
native macOS/Linux `~/.config/loomy-opencode` directory as well as the Windows
layout. It targets the bundled OpenCode engine, including Loomy's executable
copy under `~/.local/state/loomy/bin/<platform>-<arch>/opencode` when present.
Loomy's existing dead-server recovery starts the engine with the new model
catalog on a subsequent request; the desktop application can remain open.
This is an engine restart, not an in-place catalog update: ongoing engine
requests can be interrupted, and Loomy's cached liveness check can briefly
delay recovery. Unchanged configuration and temporary test roots do not
invalidate a bundled engine. Run `TestRefreshLoomyRuntimeInvalidatesNativeCatalog`
on macOS/Linux to verify the invalidation commands without stopping real apps.
If Loomy has switched its primary selection back to a native provider while
retaining the Router catalog, explicit reconnect can select the saved Router
target again. It validates the existing Router provider before reconciling the
selection against the original journal, retains an independently selected
small model, and preserves the first recovery snapshot. Provider conflicts
still block the transaction; a failed Profile save restores the actual native
selection and journal bytes.

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

When switching from Wefto's known Loomy adapter v3 to this checkout's v2,
[`checkConnectionJournalContract`](../../internal/routerhost/loomy_reconnect.go)
allows a fresh connection only if the v3 journal is marked `restored` and every
declared host file exactly matches its original bytes and existence. The new
connection captures that same baseline under the current contract in the
normal host/Profile transaction; failed Profile saves retain the v3 record and
host bytes. Active v3 records, changed snapshots and unknown contracts remain
blocked. Offline restoration of v3 records still requires the matching adapter.

`routerhost.SynchronizePorts` follows the actual service address for managed
hosts without inventing bindings or changing models. See
[host configuration](wefto-host-config.md).

## Title ownership and input

Connecting installs `api-router-title.mjs` in the selected configuration root
and adds its file URL to OpenCode's `plugin` list. This edits host configuration,
not Loomy's source, installed application or built-in providers. Adapter contract
v3 journals both files under the historical filename, retaining recovery ownership.
Validated v1 records and both released v2 contract shapes upgrade on reconnect while keeping
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
default. Only a `wefto` or legacy `api_router` source is redirected. Before its first main turn,
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

Title generation is a business request even though its purpose is `chat.title`.
Its local calls keep the request ID and actual model identity in the request
ledger; only explicit management checks use the separate diagnostic scope.
Missing historical model identity remains unknown rather than being replaced by
the model selected today.

Router selects only the latest human prompt from that envelope, using the same
Loomy projection as a main request to exclude memory, reminders and other host
preludes. It passes the prompt directly as user text, without the JSON envelope
or earlier turns, and retains its exact fragments so recognition can reuse the
main request's current-prompt cache. Context-only or malformed latest input
never falls back to older prompts. Ordinary request/model capacity limits still
apply; source text is not truncated before privacy recognition.
One shared prompt asks the selected model/group to describe the
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
`loomy_discovery_test.go` covers macOS/Linux/Windows discovery, Unicode and spaces
in home paths, explicit overrides, missing files and ambiguous Windows identities.
`TestLoomyAutoHostDefaultRootLifecycle` exercises the native platform's default
directory with no explicit override: group/model connect, reconnect, read-only
status, startup target discovery and exact disconnect restoration. Run it on
macOS for native verification; forcing the discovery branch on Windows does not
replace that check.
Consistency tests reproduce orphaned menus, nonexistent groups and Profile-write
rollback. Host tests cover retirement, exact backups and failed disconnects.
Browser tests check failed clears and stale Profiles in all five UI languages.
`TestLoomyModelNamesThroughConnectUpgradeAndRestore` covers names for new and
legacy connections, group renames, aliases, unchanged IDs, read-only status and
recovery. `TestLoomyLabelUpgradeRollsBackWithProfileAndPreservesBaseline` covers
atomic label rollback, nested model IDs and exact offline restoration. The
Agents browser suite checks the labels written by the model picker.
Title tests cover hidden input removal, model-generated greetings, stream/error
boundaries, plugin scoping, manual titles, v1 upgrades and offline removal.
Run `node --test internal/routerhost/tests/loomy-title.test.mjs` as well.

Explicit reconnect also accepts a native primary model with a retained, validated Router catalog. It selects the requested Router target, preserves an independent small_model and the first recovery baseline, and rolls back host and journal bytes when the Profile save fails. Generated provider labels upgrade from API Router or Wefto to 端云协同 in the existing transaction; provider IDs and user aliases retain their compatibility rules.
