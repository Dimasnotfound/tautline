#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

for required_command in go python node; do
  if ! command -v "$required_command" >/dev/null 2>&1; then
    printf '%s\n' "$required_command is required but was not found in PATH." >&2
    exit 1
  fi
done

UNFORMATTED=$(gofmt -l cmd)
if [ -n "$UNFORMATTED" ]; then
  printf 'Go files are not formatted:\n%s\n' "$UNFORMATTED" >&2
  exit 1
fi

python -c "import ast,pathlib; ast.parse(pathlib.Path('cmd/tautline/hermes_skill_bridge.py').read_text(encoding='utf-8')); ast.parse(pathlib.Path('scripts/generate-icon.py').read_text(encoding='utf-8'))"
for javascript in cmd/tautline/web/app.js extensions/laju-relay-bridge/background.js extensions/laju-relay-bridge/content.js extensions/laju-relay-bridge/config.example.js; do
  node --check "$javascript"
done
python -c "import base64,hashlib,json,pathlib; m=json.loads(pathlib.Path('extensions/laju-relay-bridge/manifest.json').read_text()); h=hashlib.sha256(base64.b64decode(m['key'])).digest()[:16]; eid=''.join(chr(97+(b>>4))+chr(97+(b&15)) for b in h); assert m['version']=='2.10.0' and eid=='oipiaofdfblejkognebaddegbnfaplph' and '<all_urls>' not in m['host_permissions']"

TAUTLINE_WIDGET_DOMAIN= TAUTLINE_PUBLIC_BASE_URL= go test -count=1 ./...
go vet ./...

mkdir -p bin
go build -trimpath -ldflags='-s -w' -o bin/tautline ./cmd/tautline
go build -trimpath -ldflags='-s -w' -o bin/lightpanda-shim ./cmd/lightpanda-shim

printf '%s\n' \
  "Quality gates passed." \
  "Built bin/tautline" \
  "Built bin/lightpanda-shim"
