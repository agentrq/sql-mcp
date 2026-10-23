#!/bin/sh
# Checks a Go cover profile written by `go test -coverpkg=... -coverprofile`.
#
#   scripts/coverage.sh <profile> <min-total-%> [<base-ref>]
#
# Fails when the total is below the floor. With a base ref it also fails on
# any line added since that ref (backend non-test Go files) that no test runs:
# new code is held to 100%, the AGENTS.md target, while older code is held to
# the floor until it catches up.
set -eu
profile=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
min=$2
base=${3:-}
cd "$(dirname "$0")/.."

total=$(cd backend && go tool cover -func="$profile" | awk '/^total:/ { sub("%", "", $3); print $3 }')
[ -n "$total" ] || { echo "::error::no total in $1"; exit 1; }
echo "backend coverage: ${total}% (floor ${min}%)"
if awk -v t="$total" -v m="$min" 'BEGIN { exit !(t < m) }'; then
  echo "::error::backend coverage ${total}% is below the ${min}% floor"
  exit 1
fi

[ -n "$base" ] || exit 0

module=$(awk '/^module / { print $2; exit }' backend/go.mod)
# With -coverpkg a block is listed once per test binary; it is covered when
# any of them ran it.
uncovered=$(
  git diff -U0 --no-color "$base"...HEAD -- 'backend/*.go' ':(exclude)backend/*_test.go' |
    awk -v module="$module" -v profile="$profile" '
      /^\+\+\+ b\// { file = substr($2, 3); next }
      /^@@/ {
        split($3, r, ",")
        start = substr(r[1], 2); n = (2 in r) ? r[2] : 1
        for (i = 0; i < n; i++) added[file, start + i] = 1
        next
      }
      END {
        while ((getline line < profile) > 0) {
          if (line ~ /^mode:/) continue
          split(line, f, " ")
          if (!(f[1] in hits) || f[3] > hits[f[1]]) hits[f[1]] = f[3]
        }
        for (b in hits) {
          if (hits[b] > 0) continue
          colon = index(b, ":")
          file = "backend" substr(b, length(module) + 1, colon - length(module) - 1)
          split(substr(b, colon + 1), pos, /[.,]/)
          for (l = pos[1]; l <= pos[3]; l++)
            if ((file, l) in added && !seen[file, l]++) print file ":" l
        }
      }' | sort -t: -k1,1 -k2,2n
)
if [ -n "$uncovered" ]; then
  for loc in $uncovered; do
    echo "::error file=${loc%:*},line=${loc##*:}::line added since $base is not covered by any test"
  done
  echo "lines added since $base without test coverage:"
  echo "$uncovered"
  exit 1
fi
echo "every line added since $base is covered"
