//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

// ---------------------------------------------------------------------------
// HRESULT
// ---------------------------------------------------------------------------

type HRESULT int32

const (
	S_OK    HRESULT = 0
	S_FALSE HRESULT = 1

	E_NOTIMPL     HRESULT = -2147467263 // 0x80004001
	E_NOINTERFACE HRESULT = -2147467262 // 0x80004002
	E_POINTER     HRESULT = -2147467261 // 0x80004003
	E_FAIL        HRESULT = -2147467259 // 0x80004005
	E_OUTOFMEMORY HRESULT = -2147024882 // 0x8007000E
	E_UNEXPECTED  HRESULT = -2147418113 // 0x8000FFFF
	E_INVALIDARG  HRESULT = -2147024809 // 0x80070057

	DISP_E_MEMBERNOTFOUND  HRESULT = -2147352573 // 0x80020003
	DISP_E_UNKNOWNNAME     HRESULT = -2147352570 // 0x80020006
	DISP_E_EXCEPTION       HRESULT = -2147352567 // 0x80020009
	DISP_E_BADPARAMCOUNT   HRESULT = -2147352562 // 0x8002000E
	TYPE_E_ELEMENTNOTFOUND HRESULT = -2147352077 // 0x8002802B

	REGDB_E_CLASSNOTREG       HRESULT = -2147221164 // 0x80040154
	CLASS_E_CLASSNOTAVAILABLE HRESULT = -2147221231 // 0x80040111

	OLEOBJ_S_INVALIDVERB HRESULT = 262656 // 0x00040180
)

func (h HRESULT) Failed() bool { return h < 0 }
func (h HRESULT) OK() bool     { return h >= 0 }

func (h HRESULT) Error() string {
	if n, ok := hresultNames[h]; ok {
		return fmt.Sprintf("%s (0x%08X)", n, uint32(h))
	}
	return fmt.Sprintf("HRESULT 0x%08X", uint32(h))
}

var hresultNames = map[HRESULT]string{
	E_NOTIMPL:                 "E_NOTIMPL",
	E_NOINTERFACE:             "E_NOINTERFACE",
	E_POINTER:                 "E_POINTER",
	E_FAIL:                    "E_FAIL",
	E_OUTOFMEMORY:             "E_OUTOFMEMORY",
	E_UNEXPECTED:              "E_UNEXPECTED",
	E_INVALIDARG:              "E_INVALIDARG",
	DISP_E_MEMBERNOTFOUND:     "DISP_E_MEMBERNOTFOUND",
	DISP_E_UNKNOWNNAME:        "DISP_E_UNKNOWNNAME",
	DISP_E_EXCEPTION:          "DISP_E_EXCEPTION",
	DISP_E_BADPARAMCOUNT:      "DISP_E_BADPARAMCOUNT",
	TYPE_E_ELEMENTNOTFOUND:    "TYPE_E_ELEMENTNOTFOUND",
	REGDB_E_CLASSNOTREG:       "REGDB_E_CLASSNOTREG",
	CLASS_E_CLASSNOTAVAILABLE: "CLASS_E_CLASSNOTAVAILABLE",
}

// hres converts an HRESULT into the uintptr a COM callback must return.
func hres(h HRESULT) uintptr { return uintptr(uint32(h)) }

// ---------------------------------------------------------------------------
// GUID
// ---------------------------------------------------------------------------

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func (g GUID) String() string {
	return fmt.Sprintf("{%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X}",
		g.Data1, g.Data2, g.Data3,
		g.Data4[0], g.Data4[1], g.Data4[2], g.Data4[3],
		g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7])
}

func (g GUID) Equals(o *GUID) bool {
	if o == nil {
		return false
	}
	return g.Data1 == o.Data1 && g.Data2 == o.Data2 && g.Data3 == o.Data3 && g.Data4 == o.Data4
}

// mustGUID parses "{xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx}" at init time.
func mustGUID(s string) GUID {
	var g GUID
	var d4 [8]byte
	n, err := fmt.Sscanf(s, "{%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x}",
		&g.Data1, &g.Data2, &g.Data3,
		&d4[0], &d4[1], &d4[2], &d4[3], &d4[4], &d4[5], &d4[6], &d4[7])
	if err != nil || n != 11 {
		panic("bad GUID literal: " + s)
	}
	g.Data4 = d4
	return g
}

// Standard OLE / COM interface identifiers (stable since OLE 2, Windows SDK).
var (
	IID_IUnknown                  = mustGUID("{00000000-0000-0000-C000-000000000046}")
	IID_IClassFactory             = mustGUID("{00000001-0000-0000-C000-000000000046}")
	IID_IDispatch                 = mustGUID("{00020400-0000-0000-C000-000000000046}")
	IID_IOleObject                = mustGUID("{00000112-0000-0000-C000-000000000046}")
	IID_IOleInPlaceObject         = mustGUID("{00000113-0000-0000-C000-000000000046}")
	IID_IOleWindow                = mustGUID("{00000114-0000-0000-C000-000000000046}")
	IID_IOleInPlaceUIWindow       = mustGUID("{00000115-0000-0000-C000-000000000046}")
	IID_IOleInPlaceFrame          = mustGUID("{00000116-0000-0000-C000-000000000046}")
	IID_IOleInPlaceActiveObject   = mustGUID("{00000117-0000-0000-C000-000000000046}")
	IID_IOleClientSite            = mustGUID("{00000118-0000-0000-C000-000000000046}")
	IID_IOleInPlaceSite           = mustGUID("{00000119-0000-0000-C000-000000000046}")
	IID_IPersistStreamInit        = mustGUID("{7FD52380-4E07-101B-AE2D-08002B2EC713}")
	IID_IOleControl               = mustGUID("{B196B288-BAB4-101A-B69C-00AA00341D07}")
	IID_IOleControlSite           = mustGUID("{B196B289-BAB4-101A-B69C-00AA00341D07}")
	IID_IConnectionPointContainer = mustGUID("{B196B284-BAB4-101A-B69C-00AA00341D07}")
	IID_IConnectionPoint          = mustGUID("{B196B285-BAB4-101A-B69C-00AA00341D07}")
)

// ---------------------------------------------------------------------------
// Raw vtable dispatch
// ---------------------------------------------------------------------------

const ptrSize = unsafe.Sizeof(uintptr(0))

// comCall invokes slot idx of the vtable of the COM object at this.
func comCall(this uintptr, idx uintptr, args ...uintptr) uintptr {
	if this == 0 {
		panic("comCall on nil interface pointer")
	}
	// A COM interface pointer is not Go memory: it points at a vtable pointer
	// owned by the object's implementation. go vet flags the two conversions
	// below as a possible misuse of unsafe.Pointer; they are the mechanism
	// every COM call goes through and are correct here.
	vtbl := *(*uintptr)(unsafe.Pointer(this))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + idx*ptrSize))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, this)
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return r
}

func comCallHR(this uintptr, idx uintptr, args ...uintptr) HRESULT {
	return HRESULT(int32(uint32(comCall(this, idx, args...))))
}

// IUnknown slots.
func unkQueryInterface(this uintptr, iid *GUID) (uintptr, HRESULT) {
	var out uintptr
	hr := comCallHR(this, 0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if hr.Failed() {
		return 0, hr
	}
	return out, S_OK
}

func unkAddRef(this uintptr) uint32 {
	if this == 0 {
		return 0
	}
	return uint32(comCall(this, 1))
}

func unkRelease(this uintptr) uint32 {
	if this == 0 {
		return 0
	}
	return uint32(comCall(this, 2))
}

// safeRelease releases *p and zeroes it.
func safeRelease(p *uintptr) {
	if p != nil && *p != 0 {
		unkRelease(*p)
		*p = 0
	}
}

// ---------------------------------------------------------------------------
// Strings / BSTR
// ---------------------------------------------------------------------------

func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		// The Windows APIs cannot carry an embedded NUL anyway; drop the tail.
		clean := make([]rune, 0, len(s))
		for _, r := range s {
			if r != 0 {
				clean = append(clean, r)
			}
		}
		p, _ = syscall.UTF16PtrFromString(string(clean))
	}
	return p
}

func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	const maxLen = 1 << 20
	n := 0
	for n < maxLen && *(*uint16)(unsafe.Add(unsafe.Pointer(p), 2*n)) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

func sysAllocString(s string) uintptr {
	r, _, _ := procSysAllocString.Call(uintptr(unsafe.Pointer(utf16Ptr(s))))
	return r
}

func sysFreeString(b uintptr) {
	if b != 0 {
		procSysFreeString.Call(b)
	}
}

func bstrToString(b uintptr) string {
	if b == 0 {
		return ""
	}
	n, _, _ := procSysStringLen.Call(b)
	if n == 0 {
		return ""
	}
	// b is a BSTR allocated by the OLE automation allocator, not Go memory.
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(b)), int(n)))
}

// bstrTake converts a BSTR to a Go string and frees it.
func bstrTake(b uintptr) string {
	s := bstrToString(b)
	sysFreeString(b)
	return s
}
