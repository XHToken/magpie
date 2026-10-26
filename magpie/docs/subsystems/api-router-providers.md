# API-router cloud providers and private credentials

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
See [storage](api-router-storage.md) for layout and atomic persistence.

Omitted, null or blank `apiKey`/`proxyURL` retains the saved value; `clearKey`
explicitly clears the API key. Omitted `headers`/`extraBody` retains that
section, while an explicit empty object clears it. Omitted `discoveredModels`
retains the cache; an explicit empty array clears it. Changing an endpoint or
key does not itself fetch a replacement catalogue inside the config store.

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

`providerModels` uses the configured protocol, API key, headers and proxy; it
does not detect protocols or try other providers. It requests `<base>/models`
(with `/v1` added for Anthropic when needed), with a 10-second total deadline,
2 MiB per response, at most 1000 IDs and at most 10 Anthropic pagination pages.
Redirects are refused, and failures expose safe fixed messages rather than
upstream bodies. A returned catalogue is not proof of generation access.

For management listings, `effectiveProviderModels` prefers a usable discovery
cache and otherwise uses `presetModelsForProvider`; absent both, the list is
empty. Presets come from embedded `assets/provider-presets.json`, also served
at `/provider-presets.json`. `completeProviderModelList` in
[`spark_catalog.go`](../../internal/routerservice/spark_catalog.go) supplies the
explicit official Spark catalogue completion rules. Each public provider adds
`models`, `effectiveModels` and, when available, `modelSource` (`discovered` or
`preset`). Neither these lists nor `candidateModels` changes a Profile binding.

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
are in `internal/routerservice/tests/provider-presets.test.cjs`.
