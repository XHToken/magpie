# 端云协同 service lifecycle and management console

[`cmd/wefto`](../../cmd/wefto/main.go) owns the standalone `wefto` / `wefto.exe` command. It starts independently of Magpie GUI, migrations and background jobs. Product names and narrow legacy aliases belong to [`product`](../../internal/product/identity.go).

## Startup and control

[`RunWithModelOptions`](../../internal/routerservice/service.go) initializes the selected layout, acquires the service lock, binds loopback and starts the embedded console. Fresh configuration tries `127.0.0.1:13145`; an occupied address is replaced by another available loopback port. The actual address is persisted and connected WorkBuddy/Loomy endpoints synchronize through the [host transaction owner](wefto-host-config.md). Failure rolls back that transaction and fails startup. Unrelated listeners remain running.

[`routercontrol`](../../internal/routercontrol/control.go) implements background start, status, stop, restart and explicit Windows per-user autostart. `state/router.lock/instance.json` records exact executable, PID, process stamp, instance identity and address. Control validates those against the authenticated instance; another executable owning the layout is not adopted. Only recognized exited/reused-process locks may be archived.

Executable comparisons resolve both existing absolute paths before comparing them. This recognizes Windows 8.3 aliases and macOS symlinked temporary roots returned by OS process queries. A missing or unreadable path does not establish identity; a distinct hard-link path remains another installation. PID creation stamps and authenticated instance checks still apply.

Native Windows supports external process identity and per-user autostart. Linux provides external PID identity for regression runners. Other platforms currently identify only the current foreground process: an external process remains unknown, and its lock must not be archived or its process controlled. The cleanup regression exercises that refusal on those platforms rather than spawning a child that cannot be identified.

The ordinary Windows development entry is [`scripts/start-project.ps1`](../../../scripts/start-project.ps1), also called by `start.cmd`. It uses `magpie/dist/wefto.exe` and ordinary Windows roots, and reports the actual management URL. `-CheckOnly -NoBrowser` is read-only. Its check accepts a whole fresh or legacy layout, never a mixture. Upgrades stop the original executable before switching builds; see the [upgrade guide](../wefto-upgrade.md).

## Management and model requests

[`http.go`](../../internal/routerservice/http.go) enforces loopback remote access, exact Host, same-origin requests and management Bearer authentication. The page gets its private per-run token through `POST /api/management/session`, requiring exact Origin, same-origin Fetch Metadata and a single valid `X-Wefto-UI` marker. The known `X-API-Router-UI` header is accepted at the same boundary; duplicate or conflicting markers are rejected. No CORS access is granted.

Service operations use `X-Wefto-Instance`; legacy `X-API-Router-Instance` remains accepted and either supplied value must match the process stamp. New health responses identify `wefto`. Product clients recognize the known legacy `api-router` name while retaining endpoint and instance/authentication checks.

Principal management routes are `/api/service`, `/api/service/{stop,restart,autostart}`, `/api/cloud-providers`, `/api/apps`, `/api/routing-groups`, `/api/hosts/auto`, `/api/hosts/loomy`, `/api/local-models`, `/api/redaction` and request ledger APIs. Exact allowlists/methods live in `http.go` and each handler. Model traffic uses `/app/<host>/v1/...` and its application's provider credentials; management tokens are never model credentials. See [cloud execution](wefto-cloud.md) and [applications](wefto-apps.md).

`GET`/`DELETE /api/redaction/observations` queries or explicitly clears bounded,
privacy-safe redaction observations. Group pipelines retain their request binding
and cloud fallback attempts are recorded once under their business request.

## Embedded UI

[`assets/index.html`](../../internal/routerservice/assets/index.html) and product scripts expose application, provider, routing, local-model and usage pages. Titles and header show 端云协同 with a 端 favicon. Copy and tutorial translations cover Simplified/Traditional Chinese, English, Japanese and German. Retained Magpie assets provide shared styles/icons without starting its lifecycle. The product 端 favicon overrides the shared icon collection, and its URL includes a brand revision to refresh browser caches.

Usage destinations, cloud savings, internal calls, diagnostics and retention use
the same responsive cards and detailed usage tables. Missing usage remains
unknown and explicit zero remains zero; timing details expand independently.

The initial authenticated refresh waits for `DOMContentLoaded`, after the static view scripts register their connection hooks. A fast local service must still populate the selected page when a view script loads slowly; no second navigation or manual refresh is required. Later refreshes use the same registered hooks.

UI preferences and tutorial state use `wefto.ui` and `wefto.tutorial.v1`. If the new key is absent at the same origin, the page reads its `api-router` predecessor; subsequent saves use the new key. Explicit new state wins. Another browser origin cannot read old localStorage; server credentials and models are unaffected.

Manual tutorial entry records the actual clicked entry button as its focus-return target, including on browsers that do not focus buttons on mouse clicks. Closing or dismissing the guide restores that visible entry after removing modal isolation. Programmatic and first-run entry retain the previously focused element or the ordinary guide entry fallback.

While the guide is open, Tab and Shift+Tab explicitly cycle its visible controls in both directions. This keeps focus in the dialog even when native browser keyboard preferences skip buttons. Keyboard focus may scroll the reading pane normally; entry and exit preserve the page scroll position.

Verification includes management-origin/authentication tests, legacy-header tests, service/control tests, native Windows lifecycle checks and the complete Chromium/WebKit gate in [product regression](../product-regression.md).

On the first management connection, an unseen guide opens automatically once.
The service stores the first-display marker in `Layout.State/tutorial-dismissed`,
separately from model/provider/host configuration. `/api/service` reports
`tutorialDismissed` (`false` for no marker, `true` after display, `null` if state
is unavailable); authenticated same-origin `POST /api/tutorial/dismiss` records
the marker atomically and idempotently. Restarting, changing management ports,
or opening a browser with empty/disabled storage does not force the guide back.
Existing dismissed/completed browser progress is migrated to this marker.
Unknown or corrupt state suppresses automatic opening and is preserved;
failed saves retain browser progress and retry on closing or a later management
refresh. The header/settings entries always allow manual reading, which never
resets the marker. Per-chapter progress remains in browser storage.

Service management shows process information and login startup controls without
displaying the PowerShell start command. Stop/restart confirmation acknowledges
the page disconnect; recovery messages refer to the user's usual launcher.
The service API still supplies `startCommand` for CLI lifecycle compatibility.

The embedded `tutorial.js` guide has nine chapters: introduction, providers,
Agent model selection and connection, optional local-model selection and
installation, first-request inspection, routing groups, redaction observations,
and troubleshooting/restoration (including service controls). Instructions follow
the current multi-select/automatic-save Agent UI and installed-model switch;
they distinguish host connection from verified cloud generation, experimental
local checks from quality acceptance, and group redaction from local routing.
The redaction chapter explains retained plaintext observations and deletion.
Cloud-only readers skip local installation. The first six chapter IDs and the
`wefto.tutorial.v1` storage schema remain stable, so existing reading
preferences survive; returning readers can continue into the added chapters
without a forced reopening. Guide links only navigate to existing views/tabs;
the guide only saves its display preference and never mutates configuration,
probes devices or sends model requests.

`internal/routerservice/tests/tutorial.test.cjs` checks all chapters, navigation,
previous saved progress, focus, read-only behavior, and five languages at desktop
and mobile sizes in Chromium and WebKit against an isolated candidate service.
