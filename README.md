# mymstsc — portable Remote Desktop client

`mstsc.exe` cannot simply be copied to another machine: it resolves its UI
strings from `<lang>\mstsc.exe.mui` and refuses to start without it.

`mymstsc` avoids the problem by not using `mstsc.exe` at all. The Remote Desktop
protocol stack, the rendering, the input handling and the connection UI all live
in **`%SystemRoot%\System32\mstscax.dll`**, the Remote Desktop client ActiveX
control that is part of Windows. `mstsc.exe` is only a host process for it.
`mymstsc` is a different host for the same control: a single, dependency-free
`.exe` you can copy anywhere.

```
mymstsc /v:rds01.corp.example /u:CORP\alice /w:1600 /h:900
mymstsc "\\fileserver\share\prod-jump.rdp" /f
```

## What it needs on the target machine

| Requirement | Notes |
|---|---|
| `%SystemRoot%\System32\mstscax.dll` | Ships with Windows. Present on every Server 2016–2025 and Windows 10/11 install, including Server Core. |
| Nothing else | No `mstsc.exe`, no `mstsc.exe.mui`, no runtime, no registration, no admin rights. |

`mstscax.dll.mui` supplies the localised error text. If it is missing the client
still works; disconnect reasons are then reported as numbers instead of
sentences.

## How it stays compatible from Server 2016 to Server 2025

The control's interfaces gained members with almost every Windows release. Rather
than compiling against one of them, `mymstsc` inspects the `mstscax.dll` that is
actually installed:

* **Class discovery** — the type library embedded in `mstscax.dll` is read with
  `LoadTypeLibEx(..., REGKIND_NONE)` and every `MsRdpClient*` / `MsTscAx*`
  coclass is ranked. The newest `…NotSafeForScripting` variant wins, because
  those accept a password through `ClearTextPassword` instead of always
  prompting. `mymstsc /list-classes` prints what a given machine offers.
* **No hard-coded CLSIDs or DISPIDs** — class IDs, the event interface IID and
  every event's DISPID come from that same type library, so a Windows build that
  renumbers or adds members is handled without a code change.
* **Property probing** — settings are written through `IDispatch` by name, trying
  the newest interface revision first (`AdvancedSettings9`, `…8`, … ,
  `AdvancedSettings`). A member this Windows build does not have produces a
  warning naming the setting, not a failure.
* **Non-scriptable members** — `IMsRdpClientNonScriptable*` is deliberately not
  reachable through `IDispatch`. `mymstsc` calls it through its own
  `ITypeInfo::Invoke`, which performs the marshalling from the type information.
* **Registration-free fallback** — if the control is not registered in the COM
  registry, `mymstsc` loads `mstscax.dll` and goes through `DllGetClassObject`
  directly.

`/set:` is the escape hatch for anything not covered:

```
mymstsc /v:rds01 /set:advanced.RedirectDirectX=1 /set:advanced.BitmapPersistence=0
```

## Usage

```
mymstsc [<file>.rdp] [options]
```

`mymstsc` accepts the `mstsc.exe` switches it can implement, so existing scripts
and shortcuts mostly work unchanged:

| Area | Switches |
|---|---|
| Connection | `/v:<server[:port]>` `/port:<n>` `/u:<user>` `/d:<domain>` `/p:<password>` `/prompt` |
| Display | `/f` `/w:<n>` `/h:<n>` `/span` `/multimon` `/bpp:<n>` `/scale:auto\|<percent>` `/devicescale:<percent>` `/smartsizing[:0\|1]` `/conbar:0\|1` `/noresize` `/title:<text>` |
| Session | `/admin` `/public` `/restrictedAdmin` `/remoteGuard` `/shell:<program>` `/workdir:<path>` |
| Redirection | `/drives` `/clipboard` `/printers` `/ports` `/smartcards` (each takes `:0` or `:1`), `/audio:<n>` |
| Gateway | `/g:<host>` `/gu:<user>` `/gd:<domain>` `/gp:<password>` |
| Diagnostics | `/set:<path>=<value>` `/coclass:<name>` `/list-classes` `/log:<level>` `/timestamps` `/?` `/version` |

A `.rdp` file may be given as the first argument; switches then override
individual settings from it, exactly as `mstsc` behaves. Both UTF-16 (what
`mstsc` writes) and UTF-8 files are read. A `password 51:b:` field is unwrapped
with the Windows Data Protection API, so it only decrypts for the user who saved
the file — the same boundary `mstsc` relies on.

Exit status: `0` normal disconnect, `1` error, `2` logon failure.

### Passwords

Prefer `/p:-`, which reads the password from the console with echo off, or the
`MYMSTSC_PASSWORD` / `MYMSTSC_GATEWAY_PASSWORD` environment variables. A password
in `/p:<password>` is visible in the command line of the process to anyone who
can enumerate processes on that host, and `mymstsc` warns when you use it.

If no password is available, the control raises the standard Windows credential
prompt rather than failing.

### mstsc switches that are not implemented

`/edit`, `/shadow:`, `/control`, `/noConsentPrompt`, `/migrate` and `/l` are
rejected with an explanation. Session shadowing in particular is not exposed by
the client control, so it cannot be reimplemented on top of it.

## Releases

Ready-made binaries are attached to each [release](../../releases). Download the
`.zip`, unzip it, and copy the executable wherever you need it — there is
nothing to install. The archive also carries `run.bat`, a commented launcher to
edit and keep next to the executable: it sets the server, user and window size
at the top, reads the password from the console rather than storing it, and maps
the exit status to a readable message. `SHA256SUMS.txt` carries the checksums:

```powershell
Get-FileHash .\mymstsc.exe -Algorithm SHA256
```

Releases are cut by pushing a tag: `git tag -a v1.0.0 -m "v1.0.0" && git push
origin v1.0.0`. The `Release` workflow then formats, vets, tests and
cross-compiles the tag, and publishes the archive together with generated notes.
A tag containing a hyphen (`v1.0.0-rc.1`) is published as a pre-release. The tag
name is compiled into the binary and reported by `mymstsc /version`.

The same workflow can be started by hand from the Actions tab with a version to
publish; it then creates the tag at the commit it builds. Use that where pushing
a tag directly is not possible.

The same archive can be produced locally with `./package.sh`.

## Building

No third-party modules; the standard library is all that is needed.

```sh
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always)" -o mymstsc.exe
```

Add `-H=windowsgui` to the `-ldflags` to build a version with no console window;
errors are then shown in a message box instead of on stderr. `./build.sh` does
both.

Windows Server 2016–2025 are x64 only, so `GOARCH=amd64` is the build that
matters. The code has no architecture-specific assumptions beyond that.

## Testing

The parsing layer (`.rdp` files, command line, configuration) is
platform-independent and covered by `go test ./...`, which runs on any host. The
COM layer can only be exercised on Windows.

```sh
go test ./...
go vet ./...
```

`GOOS=windows go vet` reports "possible misuse of unsafe.Pointer" at five places.
Those are the COM boundary itself — dereferencing a vtable, reading a `BSTR`, and
writing through an `[out]` pointer the control supplied. Each is commented in the
source.

## Design notes

`mymstsc` is an OLE container. It implements `IOleClientSite`,
`IOleInPlaceSite`, `IOleInPlaceFrame`, `IOleControlSite` and a stub `IDispatch`
for ambient properties, then activates the control in place inside a plain Win32
window:

```
main.go        entry point, exit codes
app.go         window, message loop, event handling, dynamic resize
control.go     control lifetime: create, embed, connect, tear down
apply.go       configuration -> control properties
olesite.go     the OLE container interfaces
sink.go        event sink (IMsTscAxEvents)
typelib.go     type library inspection: coclasses, IIDs, event names
com.go         COM primitives; dispatch.go, variant.go  IDispatch plumbing
cli.go         mstsc-compatible command line
rdpfile.go     .rdp parsing; dpapi.go  stored password decryption
win32.go       Win32 bindings, DPI awareness
```

### Full screen

`/f` connects full screen; `/multimon` and `/span` imply it. A `.rdp` file with
`screen mode id:i:2` does the same. The control owns the full-screen window
itself, including the connection bar and the Ctrl+Alt+Break toggle, exactly as
under `mstsc` — this program deliberately leaves `ContainerHandledFullScreen` at
its default rather than reimplementing that. Entering and leaving full screen at
run time is tracked, so the session stops following the container window while
it is full screen and resumes afterwards.

### High DPI

The process opts into per-monitor DPI awareness with the newest API the host
offers (`SetProcessDpiAwarenessContext` on Server 2019 and later,
`SetProcessDpiAwareness` on Server 2016, `SetProcessDPIAware` before that), so
the window is never bitmap-stretched by Windows.

Awareness alone would still leave the remote desktop rendering at 100% and
looking tiny on a scaled display, so the scale factors are handed to the session
as well:

* `/scale:auto` (the default) reads the DPI of the monitor the window is on and
  converts it to a desktop scale factor — 120 DPI becomes 125%, 144 becomes
  150%, 192 becomes 200%.
* The device scale factor is derived from it, restricted to the values the
  control accepts (100, 140, 180); `/devicescale:` overrides that choice.
* `/scale:<percent>` pins a scale instead, and `desktopscalefactor` /
  `devicescalefactor` in a `.rdp` file are honoured.
* `WM_DPICHANGED` is handled: moving the window between monitors of different
  DPI takes the rectangle Windows suggests and renegotiates the session.

The scale travels with `UpdateSessionDisplaySettings`, which means it needs
`IMsRdpClient9` or newer (Windows 8.1 / Server 2012 R2 onwards — every supported
server has it). If the control rejects a scale factor pair, the call is retried
unscaled so a rejected scale never costs the resize.

Two further details matter for behaviour:

* Keyboard input is offered to `IOleInPlaceActiveObject::TranslateAccelerator`
  before `TranslateMessage`. Without that the control never sees Tab, the arrow
  keys or its own hotkeys.
* Resizing the window calls `UpdateSessionDisplaySettings` (falling back to
  `Reconnect` on pre-`IMsRdpClient9` controls) so the remote desktop follows the
  window, like the "dynamic resolution" behaviour of `mstsc`. Use `/noresize` to
  keep a fixed session size, or `/smartsizing` to scale instead.

The process is a single-threaded apartment: the control is created, pumped and
destroyed on one OS thread (`runtime.LockOSThread`).
