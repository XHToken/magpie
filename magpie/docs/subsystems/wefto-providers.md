# 端云协同 cloud providers and private credentials

## Responsibility and source

This subsystem stores explicit cloud providers, their private credentials and
advanced options, and cached model IDs. Application Profiles own model
selection. Provider saving does not verify generation access.

- [`routerconfig/providers.go`](../../internal/routerconfig/providers.go):
  `CloudProvider`, `ProviderEdit`, `providerSecret`, `ResolvedProvider`,
  `providerFilesContext`, `providerFiles.resolved`, `CloudProvidersContext`.
- [`providers_validate.go`](../../internal/routerconfig/providers_validate.go):
  `resolveEdit`, `ResolveCloudProviderContext`, `SaveCloudProviderContext`,
  `SaveProviderDiscoveredModelsContext`.
- [`providers_write.go`](../../internal/routerconfig/providers_write.go):
  `DeleteCloudProviderContext`, `writeProviders`, `writeCredentials`,
  `ProviderError`.
- [`provider_discovery.go`](../../internal/routerconfig/provider_discovery.go):
  revision- and connection-checked catalogue results and safe failure codes.
- [`routerservice/providers_api.go`](../../internal/routerservice/providers_api.go):
  `providerAPI`, `providerInput`; HTTP registration and management authentication
  are in [`http.go`](../../internal/routerservice/http.go).
- [`providers_models.go`](../../internal/routerservice/providers_models.go):
  `providerModels`, the explicit bounded upstream model-list operation.
- [`providers.go`](../../internal/routerservice/providers.go):
  `loadProviderPresets`, `effectiveProviderModels`, `redactProvider`;
  [`providers_candidates.go`](../../internal/routerservice/providers_candidates.go)
  owns `aggregateLocalModelCandidates` and `aggregateCandidateModels`.
- [`assets/provider.js`](../../internal/routerservice/assets/provider.js): the
  Providers page, editor, save flow and explicit model-list fetch.

## Configuration and private state

`CloudProvider` has the following fields and JSON names:

| Go field | JSON name | Meaning |
| --- | --- | --- |
| `ID` | `id` | Generated `p_<hex>` identifier |
| `Revision` | `revision` | Optimistic edit revision |
| `Name` | `name` | Display name |
| `Preset` | `preset` | Optional built-in catalogue identifier |
| `Protocol` | `protocol` | Exactly `chat`, `responses` or `anthropic` |
| `BaseURL` | `baseURL` | HTTP(S) API base without credentials, query or fragment |
| `DiscoveredModels` | `discoveredModels` | Optional cached upstream model IDs |
| `ModelListError` | `modelListError` | Fixed last-refresh error code; empty after success or a connection change |
| `ProxyMode` | `proxyMode` | `system`, `direct` or `custom` |
| `HasKey` | `hasKey` | Derived credential-presence flag |
| `HeaderNames` | `headerNames` | Derived custom-header names, without values |
| `HasExtraBody` | `hasExtraBody` | Derived extra-option presence flag |
| `HasProxyURL` | `hasProxyURL` | Derived custom-proxy presence flag |
| `Model` | excluded (`json:"-"`) | Request-local legacy projection from a Profile |

The ordinary fields are stored under `config.json.providers[id]`. The four
credential-presence/header-name fields are derived on read and removed on save.
The stored record additionally contains `credentialRef: "cred_<hex>"`, managed
by the raw-document writer rather than a public struct field. Providers have no
persisted default model; the compatibility `ProviderEdit.model` is ignored.

`credentials.json` is **private plaintext JSON, not encrypted storage**. Its
`providers[credentialRef]` record contains `providerId`, `apiKey`, `headers`,
`extraBody` and optional `proxyURL`. Writes request mode `0600`; management
responses omit these values. `ResolvedProvider` is an internal private snapshot
and excludes all fields from JSON serialization.

Provider reads and writes use the shared configuration transaction lock.
Saving checks the supplied revision, increments it, creates a new immutable
credential reference, writes credentials before replacing configuration, and
keeps backups. Unknown fields survive ordinary edits. A failed configuration
replacement retains credentials required by the current configuration and its
backup. Invalid or unreadable files fail rather than becoming an empty store.
See [storage](wefto-storage.md) for layout and atomic persistence.

Omitted, null or blank `apiKey`/`proxyURL` retains the saved value; `clearKey`
explicitly clears the API key. Omitted `headers`/`extraBody` retains that
section, while an explicit empty object clears it. Omitted `discoveredModels`
retains the cache while the connection is unchanged; an explicit empty array
clears it. Changing the API base, generation protocol, effective API key,
custom headers, proxy mode or proxy URL clears discovery and its last error.
Renaming a provider and re-entering the same key preserve discovery. No edit
fetches a replacement catalogue inside the config store.

`CloudProvider.modelListError` stores only a fixed discovery error code, never
an upstream body. Persisted discovery results compare the queried revision
and complete connection snapshot before writing; a late response or an unsaved
draft cannot overwrite another connection's cache. Failed refreshes retain the
same connection's cache with a failure marker; successful refreshes clear it.

## Advanced option validation

Provider draft resolution, saving and cloud dispatch share
`routerconfig.ReservedCloudHeader` and `ReservedCloudBodyField`. Transport and
wire headers (including `Content-Type` and `Accept-Encoding`), conversation,
target, tool and remote-state overrides, and forced `!` body options are rejected
before any provider configuration or credentials are saved. This prevents an
editor from accepting options that every generation would reject at dispatch.
Ordinary custom headers and non-reserved extra fields remain supported.

## Model lists and management APIs

`providerAPI` exposes these management-authenticated routes:

- `GET /api/cloud-providers` returns `providers`, `localModels` and
  `candidateModels`; `HEAD` validates the provider read without a response body.
- `POST /api/cloud-providers` saves a `ProviderEdit` and returns
  `{ "provider": ... }` with HTTP `200` for creation and updates. The handler
  saves locally without an upstream probe.
- `DELETE /api/cloud-providers` takes JSON `{ "id": ..., "revision": ... }`.
  References from application Profiles or active routing-group members/rules
  block deletion. Successful deletion removes the provider and prunes orphan
  credential records.
- `POST /api/cloud-providers/models` resolves an editor draft or a saved
  provider and fetches its model list. `persist` and `fallback` may be supplied
  in JSON or query parameters. Successful nonempty discovery is cached only
  when `persist` is enabled for an existing provider. Fallback can use the
  saved discovery cache, built-in presets or an empty list; these alternatives
  are identified in the result's `source`/`fallback` fields.

The browser's `saveProvider` first saves, then separately calls the model-list
route with `persist` and `fallback`. A model-list failure does not undo the
successful save. No provider-level model is automatically selected.

`providerModels` uses the configured API key, headers and proxy; it does not
detect generation protocols or try other providers. The catalogue adapter in
[`provider_catalog.go`](../../internal/routerservice/provider_catalog.go)
separates directory paths, authentication, response shape and pagination from
generation. Official host and exact product-path matches use these rules:

| Official platform | Model directory |
| --- | --- |
| DeepSeek, Moonshot, Xiaomi MiMo Anthropic compatibility | Same origin `/v1/models`, Bearer |
| Baidu Qianfan pay-as-you-go Anthropic compatibility | Same origin `/v2/models`, Bearer |
| Huawei MaaS at the documented `api.modelarts-maas.com` public product paths | Same origin `/v2/models`, Bearer |
| Zhipu and Z.AI public Anthropic compatibility | Same origin `/api/paas/v4/models`, Bearer |
| xAI, StepFun, Tencent TokenHub, Ollama public roots | Same origin `/v1/models`, Bearer |
| OpenRouter | Same origin `/api/v1/models`, Bearer, without Anthropic version headers |
| Alibaba Model Studio shared and documented workspace domains | Same origin `/api/v1/models`, Bearer; `capabilities=TG`, native `output.models[].model`, `page_no`/`page_size` and `output.total` |

These catalogue rules do not change generation settings. Region-specific
hosts stay on that region, and plan paths are not rewritten to pay-as-you-go.
Custom relays retain `<base>/models` (with `/v1` added for Anthropic when
needed) and their configured protocol authentication, regardless of preset.
MiniMax and Kimi Code retain their Anthropic-native directory and auth.
Directory schemas are verified against the official references for
[Bailian](https://help.aliyun.com/en/model-studio/list-models),
[Huawei MaaS](https://support.huaweicloud.com/model-call-maas/model-call-029.html),
[Qianfan](https://cloud.baidu.com/doc/qianfan-api/s/Dmba8k71y),
[MiniMax](https://platform.minimax.io/docs/api-reference/models/anthropic/list-models)
and [Tencent TokenHub](https://intl.cloud.tencent.com/document/product/1300/78941?lang=en).
DeepSeek's fallback preset lists `deepseek-flash` and `deepseek-v4-pro`, as in
the [official model catalogue](https://api-docs.deepseek.com/api/list-models/).
Requests have a 10-second total deadline,
2 MiB per response, at most 1000 IDs and at most 10 pagination pages.
Upstream discovery uses at most eight seconds to leave time to persist timeout
status and return fallback provenance within the management deadline.
Anthropic cursors, native Bailian pages, and same-origin/same-path `links.next`
are bounded and must make progress. Foreign pagination URLs and redirects are
refused, and partial or invalid catalogues are not cached.
Redirects are refused, and failures expose safe fixed messages rather than
upstream bodies. A returned catalogue is not proof of generation access.

For management listings, `effectiveProviderModels` prefers a usable discovery
cache and otherwise uses `presetModelsForProvider`; absent both, the list is
empty. Presets come from embedded `assets/provider-presets.json`, also served
at `/provider-presets.json`. The selectable iFlytek preset is Spark MaaS
(`spark-maas`); the legacy Spark (`spark`) preset is no longer offered.
`completeProviderModelList` in
[`spark_catalog.go`](../../internal/routerservice/spark_catalog.go) supplies the
explicit official Spark catalogue completion rules. Each public provider adds
`models`, `effectiveModels` and, when available, `modelSource` (`discovered` or
`preset`) and the fixed `modelListError` if its last persisted refresh failed.
Model discovery responses additionally expose `discoveryError` with a fixed
code on fallback. Provider cards, save notices and application model pickers
distinguish discovered lists, cached lists after failure, and unverified
presets. Previously selected IDs remain selectable and are marked unverified
in their tooltip when absent from the current catalogue. Neither these lists
nor `candidateModels` changes a Profile binding. A directory can be broader
than a generation protocol or an account's entitlements; discovery is never
reported as a generation-access test.

Local candidates come from the fixed manifest, stored runtime configuration,
optional live Manager state and installation verification. They expose
`local/<model-id>` selectors and installation/selection/status fields; the
provider listing does not start a local model process.

## Verification

Run the isolated `internal/routerconfig` and `internal/routerservice` Go suites.
Provider tests cover private-field separation, revision conflicts, failed
writes, unknown-field preservation, reference-protected deletion and rejected
advanced options. Service provider/model-list tests use local fake upstreams
for discovery, caching, fallback and safe errors. Provider-page browser tests
are in `internal/routerservice/tests/provider-presets.test.cjs` and
`provider-catalogue.test.cjs`; the latter verifies provenance in both browser
engines, all five languages and narrow layouts. Catalogue tests cover official
and custom targets, native and cursor pagination, hostile next-page links,
identity invalidation, failed refreshes and late responses.
