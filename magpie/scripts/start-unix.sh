#!/bin/sh
set -eu
package_dir=$(CDPATH='' cd -P "$(dirname "$0")" && pwd)
exec "$package_dir/wefto" serve "$@"
