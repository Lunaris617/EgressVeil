#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 GOTOOLCHAIN=local
for name in controller ttl-policy; do
 cd "$root/helpers/$name"
 case "$name" in controller) dest=egressveil/controller;; ttl-policy) dest=ua3f-integration/ttl-policy;; esac
 go build -mod=vendor -trimpath -buildvcs=false -ldflags '-s -w -buildid=' -o "$root/package/luci-app-egressveil/files/usr/libexec/$dest" .
done
