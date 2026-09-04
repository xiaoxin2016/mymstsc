//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modOle32    = syscall.NewLazyDLL("ole32.dll")
	modOleAut32 = syscall.NewLazyDLL("oleaut32.dll")
	modShcore   = syscall.NewLazyDLL("shcore.dll")
	modGdi32    = syscall.NewLazyDLL("gdi32.dll")

	procGetModuleHandleW = modKernel32.NewProc("GetModuleHandleW")
	procGetSystemDirW    = modKernel32.NewProc("GetSystemDirectoryW")
	procGetConsoleWindow = modKernel32.NewProc("GetConsoleWindow")
	procGetCurrentProcID = modKernel32.NewProc("GetCurrentProcessId")

	procRegisterClassExW = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow    = modUser32.NewProc("DestroyWindow")
	procDefWindowProcW   = modUser32.NewProc("DefWindowProcW")
	procShowWindow       = modUser32.NewProc("ShowWindow")
	procUpdateWindow     = modUser32.NewProc("UpdateWindow")
	procGetMessageW      = modUser32.NewProc("GetMessageW")
	procTranslateMessage = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage  = modUser32.NewProc("PostQuitMessage")
	procPostMessageW     = modUser32.NewProc("PostMessageW")
	procGetClientRect    = modUser32.NewProc("GetClientRect")
	procAdjustWindowRect = modUser32.NewProc("AdjustWindowRectEx")
	procSetWindowPos     = modUser32.NewProc("SetWindowPos")
	procLoadCursorW      = modUser32.NewProc("LoadCursorW")
	procLoadIconW        = modUser32.NewProc("LoadIconW")
	procGetSystemMetrics = modUser32.NewProc("GetSystemMetrics")
	procSetFocus         = modUser32.NewProc("SetFocus")
	procMessageBoxW      = modUser32.NewProc("MessageBoxW")
	procSetWindowTextW   = modUser32.NewProc("SetWindowTextW")
	procIsWindow         = modUser32.NewProc("IsWindow")

	procSysAllocString = modOleAut32.NewProc("SysAllocString")
	procSysFreeString  = modOleAut32.NewProc("SysFreeString")
	procSysStringLen   = modOleAut32.NewProc("SysStringLen")
	procVariantClear   = modOleAut32.NewProc("VariantClear")
	procLoadTypeLibEx  = modOleAut32.NewProc("LoadTypeLibEx")

	procCoInitializeEx     = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize     = modOle32.NewProc("CoUninitialize")
	procCoCreateInstance   = modOle32.NewProc("CoCreateInstance")
	procCLSIDFromProgID    = modOle32.NewProc("CLSIDFromProgID")
	procOleInitialize      = modOle32.NewProc("OleInitialize")
	procOleUninitialize    = modOle32.NewProc("OleUninitialize")
	procOleSetContainedObj = modOle32.NewProc("OleSetContainedObject")

	procGetStockObject = modGdi32.NewProc("GetStockObject")
)

// Window messages.
const (
	WM_DESTROY      = 0x0002
	WM_SIZE         = 0x0005
	WM_SETFOCUS     = 0x0007
	WM_CLOSE        = 0x0010
	WM_QUIT         = 0x0012
	WM_ERASEBKGND   = 0x0014
	WM_ACTIVATEAPP  = 0x001C
	WM_SYSCOMMAND   = 0x0112
	WM_EXITSIZEMOVE = 0x0232
	WM_DPICHANGED   = 0x02E0
	WM_APP          = 0x8000

	// Application-private messages.
	wmRDPDisconnected = WM_APP + 1
	wmRDPConnected    = WM_APP + 2
	wmRDPResize       = WM_APP + 3
)

const (
	SIZE_RESTORED  = 0
	SIZE_MINIMIZED = 1
	SIZE_MAXIMIZED = 2
)

// Window styles.
const (
	CS_VREDRAW = 0x0001
	CS_HREDRAW = 0x0002
	CS_DBLCLKS = 0x0008

	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_CLIPCHILDREN     = 0x02000000
	WS_CLIPSIBLINGS     = 0x04000000
	WS_VISIBLE          = 0x10000000

	SW_HIDE       = 0
	SW_SHOWNORMAL = 1
	SW_SHOW       = 5
	SW_MINIMIZE   = 6

	SWP_NOSIZE     = 0x0001
	SWP_NOMOVE     = 0x0002
	SWP_NOZORDER   = 0x0004
	SWP_NOACTIVATE = 0x0010

	SM_CXSCREEN = 0
	SM_CYSCREEN = 1

	IDC_ARROW       = 32512
	IDI_APPLICATION = 32512

	BLACK_BRUSH = 4

	MB_OK          = 0x0000
	MB_ICONERROR   = 0x0010
	MB_ICONWARNING = 0x0030

	CW_USEDEFAULT = ^uintptr(0x7FFFFFFF) // 0x80000000 sign-extended
)

type POINT struct{ X, Y int32 }

type RECT struct{ Left, Top, Right, Bottom int32 }

func (r RECT) width() int32  { return r.Right - r.Left }
func (r RECT) height() int32 { return r.Bottom - r.Top }

type WNDCLASSEX struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type MSG struct {
	Hwnd    uintptr
	Message uint32
	_       uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
	_       uint32
}

func getModuleHandle() uintptr {
	h, _, _ := procGetModuleHandleW.Call(0)
	return h
}

func systemDirectory() string {
	buf := make([]uint16, 260)
	n, _, _ := procGetSystemDirW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) > len(buf) {
		return `C:\Windows\System32`
	}
	return syscall.UTF16ToString(buf[:n])
}

func hasConsole() bool {
	h, _, _ := procGetConsoleWindow.Call()
	return h != 0
}

func defWindowProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func destroyWindow(hwnd uintptr) {
	if hwnd != 0 {
		procDestroyWindow.Call(hwnd)
	}
}

func isWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	r, _, _ := procIsWindow.Call(hwnd)
	return r != 0
}

func showWindow(hwnd uintptr, cmd int32) {
	procShowWindow.Call(hwnd, uintptr(cmd))
}

func updateWindow(hwnd uintptr) { procUpdateWindow.Call(hwnd) }

func setFocus(hwnd uintptr) { procSetFocus.Call(hwnd) }

func postMessage(hwnd uintptr, msg uint32, wp, lp uintptr) {
	procPostMessageW.Call(hwnd, uintptr(msg), wp, lp)
}

func setWindowText(hwnd uintptr, s string) {
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(s))))
}

func clientRect(hwnd uintptr) RECT {
	var r RECT
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func systemMetric(index int32) int32 {
	r, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int32(r)
}

func messageBox(hwnd uintptr, text, caption string, flags uint32) {
	procMessageBoxW.Call(hwnd,
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(unsafe.Pointer(utf16Ptr(caption))),
		uintptr(flags))
}

// enableDPIAwareness opts the process into per-monitor DPI awareness using the
// newest API the running Windows version offers.
//
//	Windows 10 1703 / Server 2019+ : user32!SetProcessDpiAwarenessContext
//	Windows 8.1  / Server 2016     : shcore!SetProcessDpiAwareness
//	older                          : user32!SetProcessDPIAware
func enableDPIAwareness() string {
	const dpiAwarenessContextPerMonitorV2 = ^uintptr(3) // (HANDLE)-4
	if p := modUser32.NewProc("SetProcessDpiAwarenessContext"); p.Find() == nil {
		if r, _, _ := p.Call(dpiAwarenessContextPerMonitorV2); r != 0 {
			return "per-monitor-v2"
		}
	}
	if p := modShcore.NewProc("SetProcessDpiAwareness"); p.Find() == nil {
		const processPerMonitorDPIAware = 2
		if r, _, _ := p.Call(processPerMonitorDPIAware); HRESULT(int32(uint32(r))).OK() {
			return "per-monitor"
		}
	}
	if p := modUser32.NewProc("SetProcessDPIAware"); p.Find() == nil {
		if r, _, _ := p.Call(); r != 0 {
			return "system"
		}
	}
	return "unaware"
}

// dpiForWindow returns the DPI of the monitor hosting hwnd, or 96 when the
// running Windows version predates GetDpiForWindow.
func dpiForWindow(hwnd uintptr) int32 {
	if p := modUser32.NewProc("GetDpiForWindow"); p.Find() == nil {
		if r, _, _ := p.Call(hwnd); r != 0 {
			return int32(r)
		}
	}
	return 96
}

// COM apartment helpers.
const (
	COINIT_APARTMENTTHREADED = 0x2
	COINIT_DISABLE_OLE1DDE   = 0x4

	CLSCTX_INPROC_SERVER = 0x1
	CLSCTX_LOCAL_SERVER  = 0x4
)

func oleInitialize() HRESULT {
	r, _, _ := procOleInitialize.Call(0)
	return HRESULT(int32(uint32(r)))
}

func oleUninitialize() { procOleUninitialize.Call() }

func oleSetContainedObject(unk uintptr, contained bool) {
	var b uintptr
	if contained {
		b = 1
	}
	procOleSetContainedObj.Call(unk, b)
}

func coCreateInstance(clsid *GUID, ctx uint32, iid *GUID) (uintptr, HRESULT) {
	var out uintptr
	r, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(clsid)), 0, uintptr(ctx),
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	hr := HRESULT(int32(uint32(r)))
	if hr.Failed() {
		return 0, hr
	}
	return out, S_OK
}

func clsidFromProgID(progID string) (GUID, HRESULT) {
	var g GUID
	r, _, _ := procCLSIDFromProgID.Call(
		uintptr(unsafe.Pointer(utf16Ptr(progID))),
		uintptr(unsafe.Pointer(&g)))
	return g, HRESULT(int32(uint32(r)))
}
