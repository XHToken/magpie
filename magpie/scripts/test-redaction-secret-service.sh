#!/bin/sh
# Native Linux acceptance with its own D-Bus and password-protected keyring.
set -eu
if [ "${WEFTO_PRIVATE_KEYRING_CHILD:-}" != 1 ]; then
    command -v dbus-run-session >/dev/null
    command -v gnome-keyring-daemon >/dev/null
    # Pin the existing caches before replacing the test process's HOME.
    GOPATH=$("${GO_EXE:-go}" env GOPATH)
    GOMODCACHE=$("${GO_EXE:-go}" env GOMODCACHE)
    GOCACHE=$("${GO_EXE:-go}" env GOCACHE)
    export GOPATH GOMODCACHE GOCACHE
    task_root=$(mktemp -d "${TMPDIR:-/tmp}/wefto-keyring-XXXXXXXX")
    trap 'rm -rf -- "$task_root"' EXIT HUP INT TERM
    export WEFTO_PRIVATE_KEYRING_CHILD=1 WEFTO_KEYRING_TEST_ROOT="$task_root"
    # D-Bus activation inherits the bus daemon's environment as well.
    export HOME="$task_root/home" XDG_RUNTIME_DIR="$task_root/run"
    export XDG_CONFIG_HOME="$HOME/config" XDG_DATA_HOME="$HOME/data"
    export XDG_CACHE_HOME="$HOME/cache" XDG_STATE_HOME="$HOME/state"
    mkdir -p "$HOME" "$XDG_RUNTIME_DIR"
    chmod 700 "$HOME" "$XDG_RUNTIME_DIR"
    dbus-run-session -- sh "$0" "$@"
    exit $?
fi
case "$WEFTO_KEYRING_TEST_ROOT" in
    "${TMPDIR:-/tmp}"/wefto-keyring-*) ;;
    *) echo 'Refusing an unexpected keyring test directory' >&2; exit 1 ;;
esac
export HOME="$WEFTO_KEYRING_TEST_ROOT/home"
export XDG_CONFIG_HOME="$HOME/config" XDG_DATA_HOME="$HOME/data"
export XDG_CACHE_HOME="$HOME/cache" XDG_STATE_HOME="$HOME/state"
export XDG_RUNTIME_DIR="$WEFTO_KEYRING_TEST_ROOT/run"
mkdir -p "$HOME" "$XDG_RUNTIME_DIR"
chmod 700 "$HOME" "$XDG_RUNTIME_DIR"
# This fixed password protects only disposable synthetic test data. It is sent
# through stdin and is never a real user's login password or a process argument.
printf '%s\n' 'wefto-disposable-native-test-keyring' |
    gnome-keyring-daemon --unlock --components=secrets --control-directory="$XDG_RUNTIME_DIR/keyring" >/dev/null
export WEFTO_TEST_PRIVATE_KEYRING=1
trap 'gnome-keyring-daemon --quit --control-directory="$XDG_RUNTIME_DIR/keyring" >/dev/null 2>&1 || true' EXIT HUP INT TERM
"${GO_EXE:-go}" test -count=1 -v -run '^TestLinuxNativeKeyringHTTPHistory$' ./internal/routerservice
"${GO_EXE:-go}" test -count=1 -v -run '^TestLinuxNativeKeyring(RoundTrip|Child)$' ./internal/routerredact
