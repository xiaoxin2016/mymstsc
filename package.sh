#!/bin/sh
# Builds the release archive for Windows x64.
#
# VERSION defaults to the current git description; the release workflow passes
# the tag name instead.
set -eu

VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
DIST=${DIST:-dist}
NAME="mymstsc_${VERSION}_windows_amd64"
LDFLAGS="-s -w -X main.version=${VERSION}"

rm -rf "${DIST}"
mkdir -p "${DIST}/${NAME}"

echo "building ${NAME}"
GOOS=windows GOARCH=amd64 go build -trimpath \
	-ldflags "${LDFLAGS}" -o "${DIST}/${NAME}/mymstsc.exe" .
GOOS=windows GOARCH=amd64 go build -trimpath \
	-ldflags "${LDFLAGS} -H=windowsgui" -o "${DIST}/${NAME}/mymstsc-gui.exe" .

cp README.md "${DIST}/${NAME}/"
if [ -f LICENSE ]; then cp LICENSE "${DIST}/${NAME}/"; fi

(cd "${DIST}" && zip -qr "${NAME}.zip" "${NAME}")
(cd "${DIST}" && sha256sum "${NAME}.zip" "${NAME}"/*.exe > SHA256SUMS.txt)

echo
cat "${DIST}/SHA256SUMS.txt"
