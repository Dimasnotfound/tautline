#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

for command in go python node; do
  if ! command -v "$command" >/dev/null 2>&1; then
    printf '%s\n' "$command is required but was not found in PATH." >&2
    exit 1
  fi
done

UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
  printf 'Go files are not formatted:\n%s\n' "$UNFORMATTED" >&2
  exit 1
fi

python -c "import ast,pathlib; ast.parse(pathlib.Path('cmd/tautline/bridge/hermes_skill_bridge.py').read_text(encoding='utf-8'))"
node --check cmd/tautline/web/app.js

TAUTLINE_WIDGET_DOMAIN= TAUTLINE_PUBLIC_BASE_URL= go test -count=1 ./...
go vet ./...

mkdir -p bin
go build -trimpath -ldflags='-s -w' -o bin/tautline ./cmd/tautline
go build -trimpath -ldflags='-s -w' -o bin/lightpanda-shim ./cmd/lightpanda-shim

printf '%s\n' \
  "Quality gates passed." \
  "Built bin/tautline" \
  "Built bin/lightpanda-shim"
