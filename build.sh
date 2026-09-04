#!/bin/sh
# Builds mymstsc for Windows x64, with and without a console window.
set -eu

VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
OUT=${OUT:-.}
LDFLAGS="-s -w -X main.version=${VERSION}"

echo "building mymstsc ${VERSION}"
GOOS=windows GOARCH=amd64 go build -trimpath \
	-ldflags "${LDFLAGS}" -o "${OUT}/mymstsc.exe" .
GOOS=windows GOARCH=amd64 go build -trimpath \
	-ldflags "${LDFLAGS} -H=windowsgui" -o "${OUT}/mymstsc-gui.exe" .

ls -l "${OUT}/mymstsc.exe" "${OUT}/mymstsc-gui.exe"
