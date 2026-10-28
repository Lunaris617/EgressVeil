#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root/core"
go test -mod=vendor -count=1 -timeout=180s ./...
