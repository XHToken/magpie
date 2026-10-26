# API-Router Cloud Protocol Translation and Execution

## Purpose

Normalize incoming agent requests across OpenAI Chat Completions, OpenAI
Responses, and Anthropic Messages protocols, translate them to the configured
upstream provider's wire format (`chat`, `responses`, or `anthropic`), and adapt
streaming or non-streaming responses back to the caller's protocol.

## Entry Points

- Request boundary & canonical normalization (`internal/routerrequest/`):
  - `request.go`: `State`, `NewEntry`, `FromContext`, `ClassifyError`,
    `WriteError`, `ValidateModel`, `ExtractModel`
  - `normalize.go`: `Input`, `CanonicalRequest`, `Normalize`, `JudgmentInput`
- Upstream execution & protocol adaptation (`internal/routercloud/`):
  - `cloud.go`: `Target`, `Request`, `PreparedRequest`, `Result`, `Execute`,
    `ExecutePrepared`, `Prepare`, `PrepareSubpath`
- Protocol wire codecs (`internal/cloudcodec/`):
  - `codec.go`: cross-protocol request/response and streaming SSE adapters
- Service model & routing-group execution (`internal/routerservice/`):
  - `models.go`, `models_exec.go`, `models_writer.go`: `/app/{appId}/v1/*`
    entry, redaction integration, and response writing
  - `routing.go`, `group_routing.go`, `group_routing_features.go`: route
    resolution, `smart_split` / `order` / `weight` / `manual` group execution,
    inline effort & rule evaluation

## Architecture

### Request Entry and Canonical Normalization (`routerrequest`)

Incoming requests on `/app/{appId}/v1/chat/completions`,
`/app/{appId}/v1/responses`, `/app/{appId}/v1/messages`, and
`/app/{appId}/v1/messages/count_tokens` are validated by `routerrequest.NewEntry`
and parsed by `routerrequest.Normalize` into a `CanonicalRequest`:

- Resolves the application's `AppTarget` (`AppProfile` + bound models/routing
  groups + credentials) and rejects requests that use the management bearer
  token.
- Normalizes messages, system instructions, tool definitions, tool calls, and
  streaming flags across `chat`, `responses`, and `anthropic` wire formats.
- Produces `JudgmentInput` (`Judgment()`) and structural metadata used by
  `RoutingJudge` and routing-group rules (`group_routing.go` /
  `group_routing_features.go`).

### Outbound Translation and Upstream Execution (`routercloud` & `models_exec.go`)

When a cloud target (`<providerId>/<model>`) is selected directly by an
`AppProfile` or by a routing group (`executeGroupTarget` /
`executeCloudCandidate`):

1. **Redaction Hook**: If redaction is enabled for the profile or group,
   `routerredact` inspects and rewrites sensitive spans in the outbound payload
   before upstream execution and maintains the per-session token map for
   response restoration.
2. **Protocol Preparation (`routercloud.PrepareSubpath` & `cloudcodec`)**:
   - Validates and rewrites the request body to the target `Model` and upstream
     provider protocol (`chat`, `responses`, or `anthropic`), applying any
     configured `ExtraBody` fields and reasoning effort overrides.
3. **Authentication & Headers (`routercloud.ExecutePrepared`)**:
   - Injects the resolved upstream credential (`Authorization` or protocol
     authentication header) plus custom `ResolvedProvider.Headers` over the
     configured proxy mode (`direct`, `environment`, or `custom`).
4. **Response & Stream Adaptation (`routercloud` & `models_writer.go`)**:
   - Non-streaming responses and streaming SSE (`text/event-stream`) chunks are
     adapted back to the caller's wire protocol, restored through the redaction
     session map when redaction was applied, and recorded in `routerledger` and
     `routerobserve`.

## Invariants

- Standard `chat`, `responses`, and `anthropic` payloads translate end-to-end
  across cloud protocols (`cloudcodec`). Host-specific Chat Completions
  extensions (such as WorkBuddy message `agent`, Loomy top-level `$comment`,
  assistant `reasoning_content`, and custom `toolResult` fields) are preserved
  on native `chat` upstreams (`preserveChatExtensions`) and fail closed
  (`400 capability_*`) when targeting `responses` or `anthropic` upstreams so
  host semantics are never silently dropped.
- Local inference supports non-streaming across `chat`, `responses`, and
  `anthropic` (`adaptLocalJSON`) and streaming on `chat`, while failing closed
  (`local_protocol_stream_conversion_unavailable`) for `responses` or
  `anthropic` streaming requests.
- Loomy's recognized title requests use a bounded user-only title prompt and a
  complete buffered response before JSON/SSE emission. They use the ordinary
  selected model/group, validation, redaction and request budget. See
  [Loomy title ownership](api-router-loomy.md#title-ownership-and-input).
- Upstream API keys and custom provider auth headers are never echoed back to
  downstream callers or recorded in observation logs.
