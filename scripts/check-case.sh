#!/bin/sh
# Fails when two tracked paths differ only by letter case. macOS and Windows
# filesystems are case-insensitive, so such a pair is one file there: the
# checkout overwrites one with the other and shows up modified.
set -eu
cd "$(dirname "$0")/.."
collisions=$(git ls-files | awk '{ k = tolower($0); n[k]++; p[k] = p[k] "\n  " $0 } END { for (k in n) if (n[k] > 1) printf "%s", p[k] }')
if [ -n "$collisions" ]; then
  printf 'These paths collide on case-insensitive filesystems (macOS, Windows):%b\n' "$collisions" >&2
  exit 1
fi
