#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root/core"
export CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 GOTOOLCHAIN=local
go build -mod=vendor -trimpath -buildvcs=false -ldflags '-s -w -buildid=' -o ../package/luci-app-egressveil/files/usr/bin/ua3f ./cmd/ua3f
echo 'Core built. Revalidate and refresh the asset manifest before release.'
