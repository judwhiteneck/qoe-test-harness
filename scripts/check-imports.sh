#!/usr/bin/env bash
# Enforces the architecture's import boundary: the pure packages (core/compute,
# core/protocol) must not import I/O — no net, no os, and not the project's
# core/net package. See docs/ARCHITECTURE.md. Checks DIRECT imports only (stdlib
# pulls os in transitively via fmt, which is fine).
set -euo pipefail
GO="${GO:-go}"
mod="$("$GO" list -m)"
status=0

check() {
  local pkg="$1"
  [ -d "$pkg" ] || return 0   # package not created yet -> skip
  local imports
  imports="$("$GO" list -f '{{range .Imports}}{{println .}}{{end}}' "./$pkg")"
  while IFS= read -r imp; do
    [ -z "$imp" ] && continue
    case "$imp" in
      net|os|"$mod/core/net")
        echo "BOUNDARY VIOLATION: $pkg must not import '$imp'"
        status=1
        ;;
    esac
  done <<< "$imports"
}

check core/compute
check core/protocol

if [ "$status" -eq 0 ]; then
  echo "import boundaries OK"
fi
exit "$status"
