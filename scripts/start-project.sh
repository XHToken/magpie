#!/usr/bin/env bash
set -euo pipefail

# Ordinary project start/check entry for macOS and Linux.
# Reuses an active recognized installation or starts magpie/dist/wefto
# without killing unrelated port owners or mutating unrelated host settings.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
CHECK_ONLY=0
NO_BROWSER=0
USE_PROJECT_BUILD=0

for arg in "$@"; do
  case "$arg" in
    --check-only|-CheckOnly) CHECK_ONLY=1 ;;
    --no-browser|-NoBrowser) NO_BROWSER=1 ;;
    --use-project-build|-UseProjectBuild) USE_PROJECT_BUILD=1 ;;
    *)
      echo "Usage: $0 [--check-only] [--no-browser] [--use-project-build]" >&2
      exit 2
      ;;
  esac
done

PROJECT_EXE="$ROOT_DIR/magpie/dist/wefto"
DEFAULT_WEFTO="$HOME/.local/opt/wefto/wefto"
DEFAULT_LEGACY="$HOME/.local/opt/api-router/api-router"

select_exe() {
  if [[ "$USE_PROJECT_BUILD" -eq 1 ]]; then
    echo "$PROJECT_EXE"
    return
  fi
  if [[ -x "$PROJECT_EXE" ]]; then
    local state_dir
    state_dir="$("$PROJECT_EXE" dirs 2>/dev/null | python3 -c 'import json,sys; print(json.load(sys.stdin).get("state",""))' 2>/dev/null || true)"
    if [[ -n "$state_dir" && -f "$state_dir/router.lock/instance.json" ]]; then
      local owner_exe
      owner_exe="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("executable",""))' "$state_dir/router.lock/instance.json" 2>/dev/null || true)"
      if [[ -n "$owner_exe" && -x "$owner_exe" ]]; then
        echo "$owner_exe"
        return
      fi
    fi
  fi
  if [[ -x "$DEFAULT_WEFTO" ]]; then
    echo "$DEFAULT_WEFTO"
    return
  fi
  if [[ -x "$DEFAULT_LEGACY" ]]; then
    echo "$DEFAULT_LEGACY"
    return
  fi
  echo "$PROJECT_EXE"
}

EXE="$(select_exe)"

if [[ ! -x "$EXE" ]]; then
  if [[ "$CHECK_ONLY" -eq 1 ]]; then
    echo "api-router executable not found: $EXE" >&2
    exit 1
  fi
  if command -v go >/dev/null 2>&1; then
    make -C "$ROOT_DIR/magpie" wefto >&2
    EXE="$PROJECT_EXE"
  else
    ARGS=()
    [[ "$NO_BROWSER" -eq 1 ]] && ARGS+=(--no-browser)
    exec python3 "$ROOT_DIR/skills/api-router-start/scripts/prepare-unix.py" "${ARGS[@]}"
  fi
fi

SUBCMD="start"
[[ "$CHECK_ONLY" -eq 1 ]] && SUBCMD="status"

STATUS_JSON="$("$EXE" service "$SUBCMD")"
echo "$STATUS_JSON"

if [[ "$CHECK_ONLY" -eq 0 && "$NO_BROWSER" -eq 0 ]]; then
  URL="$(printf '%s' "$STATUS_JSON" | python3 -c 'import json,sys; d=json.load(sys.stdin); a=d.get("address",""); print(d.get("managementURL") or (f"http://{a}/" if a else ""))' 2>/dev/null || true)"
  if [[ -n "$URL" ]]; then
    if command -v open >/dev/null 2>&1; then
      open "$URL" >/dev/null 2>&1 || true
    elif command -v xdg-open >/dev/null 2>&1; then
      xdg-open "$URL" >/dev/null 2>&1 || true
    fi
  fi
fi
