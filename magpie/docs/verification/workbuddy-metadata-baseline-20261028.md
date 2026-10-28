# Local baseline regressions observed during the metadata audit

Status: open, outside the request-metadata compatibility change. No external
issue or message was posted. Production/fixture changes for these cases were
not included in the compatibility patch.

Both cases reproduce on local baseline b44f8831. A Go overlay restores all
changed Go files and the two affected test files from that commit, then runs:

    go test -tags nogui ./internal/agent ./internal/gateway \
      -run '^TestGrokInstalled$|^TestSearchFollowsTheSearchersProxy$' \
      -count=1 -timeout=3m

Environment: WSL Ubuntu-D, Go 1.26.8, a Linux-only PATH, isolated HOME/XDG/APPDATA
and task-specific TMPDIR under /var/tmp, pinned Go caches and no registry
network access. Both tests fail identically with and without the metadata fix.

## TestGrokInstalled

`internal/agent/cliupdate_test.go:486` reports post-update verification failure:
the fake `.grok/bin/grok` prints `Invalid argument` under WSL.

The test writes a two-byte `MZ` placeholder and mocks `runVersion`/`runUpdate`.
`UpdateCLI` also calls `runsBroken` -> `runCheck`, which is not mocked here.
On WSL, PE interop attempts to execute that placeholder and gives it an actual
nonzero exit. This differs from a plain Linux failed-to-start result. Mocking
the post-update probe alongside the other fixture probes makes this test pass;
that experiment was reverted to keep this patch focused.

## TestSearchFollowsTheSearchersProxy

`internal/gateway/search_proxy_test.go:291` fails in the final "the search API,
Auto" case. Tavily's `https://api.test/tavily/search` is dialed directly and the
CONNECT proxy records no request. The fixture's direct dial deliberately
returns a synthetic timeout; no real vendor is contacted.

After the Kimi case the fixture assigns `st.Proxy = ""` without saving it.
Saving that setting alone does not resolve the failure, so it must not be
presented as a complete fix. The separate search API's effective proxy/context
path needs tracing. Both the save-only experiment and the unrelated test edits
were reverted. The metadata change neither modifies search proxy selection nor
resolves this existing failure.
