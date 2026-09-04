#!/bin/sh
# Writes the release notes for a tag to standard output.
#
# Usage: release-notes.sh <tag>
set -eu

TAG=${1:?usage: release-notes.sh <tag>}

# On a tag push the tag exists and the log runs up to it. On a manual release
# the tag has not been created yet, so the log runs up to the commit being
# built instead.
if git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null 2>&1; then
	REF="${TAG}"
	PREV=$(git describe --tags --abbrev=0 "${TAG}^" 2>/dev/null || true)
else
	REF=HEAD
	PREV=$(git describe --tags --abbrev=0 2>/dev/null || true)
fi

cat <<'HEAD'
## Install

Download the archive below, unzip it, and copy the executable wherever you need
it. It is self-contained: no installer, no runtime, no COM registration and no
administrator rights.

| File | Use |
|---|---|
| `mymstsc.exe` | console build; logs go to stderr |
| `mymstsc-gui.exe` | no console window; errors appear in a message box |

The only requirement on the target machine is
`%SystemRoot%\System32\mstscax.dll`, the Remote Desktop client control that
ships with Windows. It is present on every Windows Server 2016-2025 and
Windows 10/11 install, Server Core included. Neither `mstsc.exe` nor
`mstsc.exe.mui` is needed.

Run `mymstsc /?` for the options, or `mymstsc /list-classes` to see which
control classes a given machine offers.

## Verify

`SHA256SUMS.txt` lists the checksums of the archive and of each executable
inside it. In PowerShell:

```powershell
Get-FileHash .\mymstsc.exe -Algorithm SHA256
```

HEAD

printf '## Changes\n\n'
if [ -n "${PREV}" ]; then
	printf 'Since %s:\n\n' "${PREV}"
	git log --no-merges --pretty=format:'- %s (%h)' "${PREV}..${REF}"
	printf '\n'
else
	printf 'First release.\n\n'
	git log --no-merges --pretty=format:'- %s (%h)' "${REF}"
	printf '\n'
fi
