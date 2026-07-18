#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

mkdir -p bin
go fmt ./...
go test ./...
go vet ./...
go build -trimpath -ldflags='-s -w' -o bin/devspace ./cmd/devspace

echo "Built bin/devspace"
