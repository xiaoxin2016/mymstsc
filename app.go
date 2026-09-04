//go:build windows

package main

import (
	"strings"
	"syscall"
	"unsafe"
)

// App owns the container window and the embedded control.
type App struct {
	cfg  *Config
	hwnd uintptr
	site *oleSite
	ctl  *rdpControl
	sink *eventSink

	warnings []string

	connected      bool
	everConnected  bool
	loginComplete  bool
	disconnectSeen bool
	logonError     bool
	fatalError     bool
	closing        bool

	discReason   int32
	discExtended int32
	discMessage  string

	pendingResize bool
	sessionW      int32
	sessionH      int32
	resizeWarned  bool

	exitCode int
}

var theApp *App

// windowClassName is registered once per process.
const windowClassName = "MyMstscHostWindow"

// Run performs the whole connection lifecycle and returns the process exit code.
func (a *App) Run() (int, error) {
	cfg := a.cfg

	mode := enableDPIAwareness()
	logDebugf("DPI awareness: %s", mode)

	if hr := oleInitialize(); hr.Failed() {
		return 1, errf("OleInitialize: %s", hr.Error())
	}
	defer oleUninitialize()

	ctl, err := newRDPControl(cfg.CoclassName)
	if err != nil {
		return 1, err
	}
	a.ctl = ctl
	// One cleanup path, in the order OLE expects: stop events, tear the
	// control down, then release the container it points at.
	defer a.cleanup()

	a.resolveSize()

	if err := a.createWindow(); err != nil {
		return 1, err
	}

	rc := clientRect(a.hwnd)
	if err := ctl.Embed(a.site, a.hwnd, rc); err != nil {
		return 1, err
	}

	if ctl.eventTI != 0 {
		a.sink = newEventSink(a, ctl.eventIID, ctl.eventNames)
		if err := ctl.AdviseEvents(a.sink); err != nil {
			logWarnf("event notifications unavailable: %v", err)
			a.sink.dispose()
			a.sink = nil
		}
	}

	warnings, err := applySettings(ctl, cfg)
	a.warnings = warnings
	if err != nil {
		return 1, err
	}

	if cfg.FullScreen {
		if err := ctl.setAny(true, "FullScreen"); err != nil {
			logWarnf("full screen could not be requested: %v", err)
		}
	}

	showWindow(a.hwnd, SW_SHOWNORMAL)
	updateWindow(a.hwnd)

	logInfof("connecting to %s%s ...", cfg.Server, portSuffix(cfg.Port))
	if err := ctl.Connect(); err != nil {
		return 1, errf("Connect: %w", err)
	}

	a.messageLoop()

	// Drop our references to the credentials as soon as the session is over.
	// Go strings are immutable, so this releases the copies for collection
	// rather than overwriting them in place.
	cfg.Password = ""
	cfg.Gateway.Password = ""

	a.reportOutcome()
	return a.exitCode, nil
}

// cleanup tears down the sink, the control and the container site in order.
func (a *App) cleanup() {
	if a.sink != nil {
		a.sink.dispose()
		a.sink = nil
	}
	if a.ctl != nil {
		a.ctl.Close()
		a.ctl = nil
	}
	if a.site != nil {
		a.site.dispose()
		a.site = nil
	}
}

func portSuffix(p int) string {
	if p == 0 {
		return ""
	}
	return ":" + itoa(p)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// resolveSize fills in a session size when none was requested.
func (a *App) resolveSize() {
	cfg := a.cfg
	sw, sh := systemMetric(SM_CXSCREEN), systemMetric(SM_CYSCREEN)
	if sw <= 0 {
		sw = 1024
	}
	if sh <= 0 {
		sh = 768
	}
	if cfg.Width == 0 {
		if cfg.FullScreen {
			cfg.Width = int(sw)
		} else {
			cfg.Width = clampInt(int(float64(sw)*0.8), 800, 1920)
		}
	}
	if cfg.Height == 0 {
		if cfg.FullScreen {
			cfg.Height = int(sh)
		} else {
			cfg.Height = clampInt(int(float64(sh)*0.8), 600, 1200)
		}
	}
	// The RDP protocol wants even dimensions.
	cfg.Width &^= 1
	cfg.Height &^= 1
	a.sessionW, a.sessionH = int32(cfg.Width), int32(cfg.Height)
	logDebugf("session size %dx%d (screen %dx%d)", cfg.Width, cfg.Height, sw, sh)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// createWindow registers the window class and creates the container window
// sized so that its client area matches the session.
func (a *App) createWindow() error {
	inst := getModuleHandle()
	cursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
	icon, _, _ := procLoadIconW.Call(0, IDI_APPLICATION)
	brush, _, _ := procGetStockObject.Call(BLACK_BRUSH)

	wc := WNDCLASSEX{
		Size:       uint32(unsafe.Sizeof(WNDCLASSEX{})),
		Style:      CS_HREDRAW | CS_VREDRAW | CS_DBLCLKS,
		WndProc:    syscall.NewCallback(wndProc),
		Instance:   inst,
		Icon:       icon,
		IconSm:     icon,
		Cursor:     cursor,
		Background: brush,
		ClassName:  utf16Ptr(windowClassName),
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return errf("RegisterClassEx: %v", err)
	}

	style := uintptr(WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN | WS_CLIPSIBLINGS)
	rc := RECT{0, 0, int32(a.cfg.Width), int32(a.cfg.Height)}
	procAdjustWindowRect.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0)

	w, h := rc.width(), rc.height()
	sw, sh := systemMetric(SM_CXSCREEN), systemMetric(SM_CYSCREEN)
	x, y := (sw-w)/2, (sh-h)/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr(windowClassName))),
		uintptr(unsafe.Pointer(utf16Ptr(a.cfg.Title))),
		style,
		uintptr(uint32(x)), uintptr(uint32(y)),
		uintptr(uint32(w)), uintptr(uint32(h)),
		0, 0, inst, 0)
	if hwnd == 0 {
		return errf("CreateWindowEx: %v", err)
	}
	a.hwnd = hwnd
	a.site = newOleSite(a)
	return nil
}

// setControlRect is called both from WM_SIZE and from the control's own
// IOleInPlaceSite::OnPosRectChange.
func (a *App) setControlRect(rc RECT) {
	if a.ctl != nil {
		a.ctl.SetRect(rc)
	}
}

// activeObject returns the control's IOleInPlaceActiveObject, whichever way we
// obtained it.
func (a *App) activeObject() uintptr {
	if a.site != nil && a.site.activeObject != 0 {
		return a.site.activeObject
	}
	if a.ctl != nil {
		return a.ctl.activeObject
	}
	return 0
}

// messageLoop runs until the window is destroyed. Keyboard input is offered to
// the control first; without this the control never sees Tab, the arrow keys or
// its own accelerators.
func (a *App) messageLoop() {
	var msg MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		ret := int32(r)
		if ret == 0 || ret == -1 {
			return
		}
		if ao := a.activeObject(); ao != 0 {
			if comCallHR(ao, 5, uintptr(unsafe.Pointer(&msg))) == S_OK {
				continue
			}
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// wndProc dispatches window messages to the single App instance.
func wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	a := theApp
	if a == nil {
		return defWindowProc(hwnd, msg, wp, lp)
	}
	switch msg {
	case WM_SIZE:
		if wp == SIZE_MINIMIZED {
			break
		}
		rc := clientRect(hwnd)
		a.setControlRect(rc)
		if wp == SIZE_MAXIMIZED || wp == SIZE_RESTORED {
			a.pendingResize = true
			// A maximise or restore is a finished gesture; a drag is not, and
			// is handled on WM_EXITSIZEMOVE instead.
			if wp == SIZE_MAXIMIZED {
				a.applyDynamicResize()
			}
		}
		return 0

	case WM_EXITSIZEMOVE:
		a.applyDynamicResize()
		return 0

	case WM_SETFOCUS:
		a.focusControl()
		return 0

	case WM_ACTIVATEAPP:
		// The control needs to know when the frame gains or loses activation,
		// otherwise it keeps the keyboard hooked while another app is in front.
		if ao := a.activeObject(); ao != 0 {
			comCall(ao, 6, wp) // IOleInPlaceActiveObject::OnFrameWindowActivate
		}
		return 0

	case WM_ERASEBKGND:
		// The control paints the whole client area; skip the flicker.
		return 1

	case wmRDPConnected:
		setWindowText(hwnd, a.cfg.Title)
		return 0

	case wmRDPDisconnected:
		if !a.closing {
			a.closing = true
			destroyWindow(hwnd)
		}
		return 0

	case WM_CLOSE:
		if a.connected && !a.closing && a.sink != nil {
			a.closing = true
			logInfof("disconnecting ...")
			if err := a.ctl.Disconnect(); err != nil {
				logWarnf("Disconnect: %v", err)
				destroyWindow(hwnd)
			}
			// The window is destroyed once OnDisconnected arrives. Without an
			// event sink there is nothing to wait for, so the branch below
			// closes straight away instead.
			return 0
		}
		if a.connected && a.ctl != nil {
			if err := a.ctl.Disconnect(); err != nil {
				logWarnf("Disconnect: %v", err)
			}
		}
		a.closing = true
		destroyWindow(hwnd)
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}
	return defWindowProc(hwnd, msg, wp, lp)
}

// focusControl hands the keyboard to the control's own window.
func (a *App) focusControl() {
	if a.ctl == nil || a.ctl.inPlaceObject == 0 {
		return
	}
	var child uintptr
	if hr := comCallHR(a.ctl.inPlaceObject, 3, uintptr(unsafe.Pointer(&child))); hr.OK() && child != 0 {
		setFocus(child)
	}
}

// applyDynamicResize asks the server to match the window size, which is what
// mstsc does when "dynamic resolution" is on.
func (a *App) applyDynamicResize() {
	if !a.pendingResize {
		return
	}
	a.pendingResize = false
	if !a.cfg.DynamicResize || !a.connected || a.cfg.FullScreen {
		return
	}
	rc := clientRect(a.hwnd)
	w, h := rc.width()&^1, rc.height()&^1
	if w < 200 || h < 200 || (w == a.sessionW && h == a.sessionH) {
		return
	}
	if err := a.ctl.UpdateSessionDisplaySettings(w, h); err != nil {
		logDebugf("UpdateSessionDisplaySettings: %v", err)
		// Controls older than IMsRdpClient9 only offer Reconnect, which
		// renegotiates the session at the new size.
		if err2 := a.ctl.Reconnect(w, h); err2 != nil {
			if !a.resizeWarned {
				a.resizeWarned = true
				logWarnf("this control cannot resize a live session (%v); "+
					"use /smartsizing to scale the session instead", err)
			}
			return
		}
	}
	a.sessionW, a.sessionH = w, h
	logDebugf("session resized to %dx%d", w, h)
}

// onEvent receives every event the control raises. Names come from the type
// library, so unknown members are logged rather than misinterpreted.
func (a *App) onEvent(id int32, name string, args []VARIANT) {
	switch name {
	case "OnConnecting":
		logDebugf("connecting")
	case "OnConnected":
		a.connected = true
		a.everConnected = true
		logInfof("connected to %s", a.cfg.Server)
		postMessage(a.hwnd, wmRDPConnected, 0, 0)
	case "OnLoginComplete":
		a.loginComplete = true
		logInfof("logon complete")
	case "OnDisconnected":
		reason, _ := argInt(args, 0)
		a.handleDisconnect(reason)
	case "OnFatalError":
		code, _ := argInt(args, 0)
		a.fatalError = true
		logErrorf("the client control reported a fatal error (code %d)", code)
	case "OnLogonError":
		// OnLogonError also carries informational logon events, so this is
		// only treated as a failure if the logon never completed.
		code, _ := argInt(args, 0)
		a.logonError = true
		logWarnf("logon event reported by the control (code %d)", code)
	case "OnWarning":
		code, _ := argInt(args, 0)
		logWarnf("client control warning (code %d)", code)
	case "OnAutoReconnecting", "OnAutoReconnecting2":
		reason, _ := argInt(args, 0)
		logWarnf("connection lost, reconnecting (reason %d)", reason)
	case "OnAutoReconnected":
		logInfof("reconnected")
	case "OnEnterFullScreenMode", "OnRequestGoFullScreen":
		logDebugf("entering full screen")
	case "OnLeaveFullScreenMode", "OnRequestLeaveFullScreen":
		logDebugf("leaving full screen")
	case "OnRequestContainerMinimize":
		showWindow(a.hwnd, SW_MINIMIZE)
	case "OnConfirmClose":
		// The single out parameter decides whether the close proceeds.
		setOutBool(args, 0, true)
	case "OnRemoteDesktopSizeChange":
		w, _ := argInt(args, 0)
		h, _ := argInt(args, 1)
		if w > 0 && h > 0 {
			a.sessionW, a.sessionH = w, h
			logDebugf("remote desktop is now %dx%d", w, h)
		}
	case "OnIdleTimeoutNotification":
		logInfof("the session was idle for too long")
	case "OnServiceMessageReceived":
		logInfof("server message: %s", argString(args, 0))
	case "":
		logDebugf("event dispid %d (name unknown)", id)
		a.pollConnectionState()
	default:
		logDebugf("event %s (dispid %d)", name, id)
	}
}

// handleDisconnect records why the session ended and tears the window down.
func (a *App) handleDisconnect(reason int32) {
	if a.disconnectSeen {
		return
	}
	a.disconnectSeen = true
	a.connected = false
	a.discReason = reason
	a.discExtended = a.ctl.ExtendedDisconnectReason()
	a.discMessage = a.ctl.ErrorDescription(reason, a.discExtended)
	postMessage(a.hwnd, wmRDPDisconnected, 0, 0)
}

// pollConnectionState is the fallback used when the event names could not be
// read from the type library: any event triggers a look at Connected.
func (a *App) pollConnectionState() {
	if a.disconnectSeen {
		return
	}
	switch a.ctl.ConnectedState() {
	case 1:
		a.connected = true
		a.everConnected = true
	case 0:
		if a.everConnected {
			a.handleDisconnect(0)
		}
	}
}

// setOutBool writes into a [out] VARIANT_BOOL* argument if the control passed
// one by reference.
func setOutBool(args []VARIANT, i int, value bool) {
	if i < 0 || i >= len(args) {
		return
	}
	v := &args[i]
	if v.VT != (VT_BOOL|VT_BYREF) || v.data[0] == 0 {
		return
	}
	// The control passed the address of its own VARIANT_BOOL.
	p := (*int16)(unsafe.Pointer(v.data[0]))
	if value {
		*p = -1
	} else {
		*p = 0
	}
}

// reportOutcome prints the result of the session and sets the exit code.
func (a *App) reportOutcome() {
	switch {
	case a.logonError && !a.loginComplete:
		a.exitCode = 2
	case a.fatalError:
		a.exitCode = 1
	case !a.everConnected:
		a.exitCode = 1
	default:
		a.exitCode = 0
	}

	msg := strings.TrimSpace(a.discMessage)
	switch {
	case msg != "" && a.exitCode == 0:
		logInfof("session ended: %s", msg)
	case msg != "":
		logErrorf("%s", msg)
	case a.everConnected:
		logInfof("session ended")
	default:
		logErrorf("the connection to %s failed (disconnect reason %d, extended %d)",
			a.cfg.Server, a.discReason, a.discExtended)
	}
	if a.discReason != 0 || a.discExtended != 0 {
		logDebugf("disconnect reason %d, extended reason %d", a.discReason, a.discExtended)
	}
	if a.exitCode != 0 && len(a.warnings) > 0 {
		logInfof("settings that could not be applied may be relevant:")
		for _, w := range a.warnings {
			logInfof("  - %s", w)
		}
	}
}
