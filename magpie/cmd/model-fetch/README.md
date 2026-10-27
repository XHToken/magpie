# Fixed model acquisition

Internal acceptance tool; the production catalog and download defaults stay
fixed. `--model` selects `spark-x2.6-4B` (Q4_K_M) or
`spark-x2.6-4B-Q8_0` from `routermanifest.Fixed`.

```powershell
go build -o D:/temp/model-fetch.exe ./cmd/model-fetch
D:/temp/model-fetch.exe --model spark-x2.6-4B --cache D:/temp/wefto-real-model-cache --workers 8 --attempts 3 --attempt-timeout 15m
```

`--workers 0` uses direct HTTP/1.1. `1..8` uses persistent 4 MiB ranges from the
same fixed mirror/revision. Each range has at most three 60-second attempts,
strict 206/Content-Range/length checks and bounded HTTPS Hugging Face redirects.
Signed CDN addresses are excluded from logs. Completed chunks have local SHA256
receipts and are rehashed before reuse; interrupted chunks retain their prefix.
The primary `.part` and metadata keep the original artifact URL and ETag when
switching strategy. The outer production downloader alone verifies the complete
fixed size/SHA256 and publishes the final `.gguf`; chunks are never installation
proof. A new invocation resumes saved progress. Do not delete the cache to retry.

The production downloader falls back from HF-Mirror to pinned ModelScope URLs
on remote transfer/integrity failures. The range transport only handles the
original HF-Mirror URL; ModelScope uses the normal resumable HTTP transfer.
Changing source resets the primary partial and its validators, while preserving
the separate HF range cache. JSONL progress includes the current source `host`.

Cache files are `<cache>/<model-id>/<fixed filename>`. Range state is under
`<fixed filename>.ranges/<full artifact SHA256>/`. This additionally retains up
to one model's worth of chunks, so reserve at least two model copies for
acquisition, plus the separate installed copy. Actual disk/write errors fail
the attempt and preserve completed state; the production downloader's initial
space probe does not account for the additional range cache.

After the bounded attempt limit, inspect the JSONL errors/progress and rerun
only when useful; no background retry service is installed. A manually obtained
complete file belongs at the exact final cache filename, never `.part`; the
next invocation rehashes it. The acceptance install command independently
rehashes all source and installed bytes again.

Tests use local synthetic range servers, including out-of-order completion,
short-read resume, refused full responses, invalid ranges and cancellation.
No ordinary unit test reaches a model repository.
