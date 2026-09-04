//go:build windows

package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unsafe"
)

// TYPEKIND values (oaidl.h).
const (
	TKIND_ENUM = iota
	TKIND_RECORD
	TKIND_MODULE
	TKIND_INTERFACE
	TKIND_DISPATCH
	TKIND_COCLASS
	TKIND_ALIAS
	TKIND_UNION
)

// IMPLTYPEFLAGS (oaidl.h).
const (
	IMPLTYPEFLAG_FDEFAULT = 0x1
	IMPLTYPEFLAG_FSOURCE  = 0x2
)

// REGKIND values (oleauto.h).
const (
	REGKIND_DEFAULT  = 0
	REGKIND_REGISTER = 1
	REGKIND_NONE     = 2
)

type TYPEDESC struct {
	Union uintptr
	VT    uint16
	_     uint16
	_     uint32
}

type IDLDESC struct {
	Reserved uintptr
	Flags    uint16
	_        uint16
	_        uint32
}

// TYPEATTR mirrors the Win32 TYPEATTR.
type TYPEATTR struct {
	GUID             GUID
	LCID             uint32
	Reserved         uint32
	MemidConstructor int32
	MemidDestructor  int32
	LpstrSchema      uintptr
	CbSizeInstance   uint32
	Typekind         uint32
	CFuncs           uint16
	CVars            uint16
	CImplTypes       uint16
	CbSizeVft        uint16
	CbAlignment      uint16
	WTypeFlags       uint16
	WMajorVerNum     uint16
	WMinorVerNum     uint16
	TdescAlias       TYPEDESC
	IdldescType      IDLDESC
}

// funcDescHead is the leading part of a Win32 FUNCDESC. Only the member id is
// read, so the trailing fields are deliberately not declared.
type funcDescHead struct {
	Memid int32
}

// tlType is one type description inside a type library.
type tlType struct {
	Name string
	Kind uint32
	GUID GUID
	ti   uintptr // ITypeInfo, owned by the parent typeLib
}

// typeLib wraps an ITypeLib and caches its type descriptions.
type typeLib struct {
	p      uintptr
	Path   string
	Types  []*tlType
	byName map[string]*tlType
}

// loadTypeLib reads the type library embedded in a module without touching the
// registry (REGKIND_NONE), so it also works on a machine where the control was
// never registered.
func loadTypeLib(path string) (*typeLib, error) {
	var p uintptr
	r, _, _ := procLoadTypeLibEx.Call(
		uintptr(unsafe.Pointer(utf16Ptr(path))),
		REGKIND_NONE,
		uintptr(unsafe.Pointer(&p)))
	if hr := HRESULT(int32(uint32(r))); hr.Failed() {
		return nil, fmt.Errorf("LoadTypeLibEx(%s): %s", path, hr.Error())
	}

	tl := &typeLib{p: p, Path: path, byName: map[string]*tlType{}}
	count := uint32(comCall(p, 3)) // ITypeLib::GetTypeInfoCount returns the count
	for i := uint32(0); i < count; i++ {
		var kind uint32
		if hr := comCallHR(p, 5, uintptr(i), uintptr(unsafe.Pointer(&kind))); hr.Failed() {
			continue
		}
		var nameB uintptr
		if hr := comCallHR(p, 9, uintptr(i),
			uintptr(unsafe.Pointer(&nameB)), 0, 0, 0); hr.Failed() {
			continue
		}
		name := bstrTake(nameB)

		var ti uintptr
		if hr := comCallHR(p, 4, uintptr(i), uintptr(unsafe.Pointer(&ti))); hr.Failed() {
			continue
		}
		g, err := typeInfoGUID(ti)
		if err != nil {
			unkRelease(ti)
			continue
		}
		t := &tlType{Name: name, Kind: kind, GUID: g, ti: ti}
		tl.Types = append(tl.Types, t)
		if _, dup := tl.byName[name]; !dup {
			tl.byName[name] = t
		}
	}
	if len(tl.Types) == 0 {
		tl.Close()
		return nil, fmt.Errorf("type library %s contains no usable type information", path)
	}
	return tl, nil
}

func (t *typeLib) Close() {
	for _, e := range t.Types {
		if e.ti != 0 {
			unkRelease(e.ti)
			e.ti = 0
		}
	}
	t.Types = nil
	t.byName = nil
	safeRelease(&t.p)
}

func (t *typeLib) Find(name string) *tlType { return t.byName[name] }

// typeInfoGUID reads the GUID out of an ITypeInfo.
func typeInfoGUID(ti uintptr) (GUID, error) {
	var pattr *TYPEATTR
	if hr := comCallHR(ti, 3, uintptr(unsafe.Pointer(&pattr))); hr.Failed() || pattr == nil {
		return GUID{}, fmt.Errorf("GetTypeAttr: %s", hr.Error())
	}
	g := pattr.GUID
	comCall(ti, 19, uintptr(unsafe.Pointer(pattr))) // ReleaseTypeAttr
	return g, nil
}

func typeInfoAttr(ti uintptr) (TYPEATTR, error) {
	var pattr *TYPEATTR
	if hr := comCallHR(ti, 3, uintptr(unsafe.Pointer(&pattr))); hr.Failed() || pattr == nil {
		return TYPEATTR{}, fmt.Errorf("GetTypeAttr: %s", hr.Error())
	}
	attr := *pattr
	comCall(ti, 19, uintptr(unsafe.Pointer(pattr)))
	return attr, nil
}

// DefaultSource returns the ITypeInfo of the coclass' default outgoing
// (event) interface. The caller must release the returned pointer.
func (t *tlType) DefaultSource() (uintptr, GUID, error) {
	attr, err := typeInfoAttr(t.ti)
	if err != nil {
		return 0, GUID{}, err
	}
	var fallback uintptr
	var fallbackGUID GUID
	for i := uint16(0); i < attr.CImplTypes; i++ {
		var flags int32
		if hr := comCallHR(t.ti, 9, uintptr(i), uintptr(unsafe.Pointer(&flags))); hr.Failed() {
			continue
		}
		if flags&IMPLTYPEFLAG_FSOURCE == 0 {
			continue
		}
		var href uint32
		if hr := comCallHR(t.ti, 8, uintptr(i), uintptr(unsafe.Pointer(&href))); hr.Failed() {
			continue
		}
		var ti uintptr
		if hr := comCallHR(t.ti, 14, uintptr(href), uintptr(unsafe.Pointer(&ti))); hr.Failed() {
			continue
		}
		g, err := typeInfoGUID(ti)
		if err != nil {
			unkRelease(ti)
			continue
		}
		if flags&IMPLTYPEFLAG_FDEFAULT != 0 {
			if fallback != 0 {
				unkRelease(fallback)
			}
			return ti, g, nil
		}
		if fallback == 0 {
			fallback, fallbackGUID = ti, g
		} else {
			unkRelease(ti)
		}
	}
	if fallback != 0 {
		return fallback, fallbackGUID, nil
	}
	return 0, GUID{}, fmt.Errorf("%s declares no source interface", t.Name)
}

// MemberNames maps every DISPID of an interface to its declared name. This is
// how the event sink stays correct across control versions instead of relying
// on a hard-coded DISPID table.
func memberNames(ti uintptr) map[int32]string {
	out := map[int32]string{}
	attr, err := typeInfoAttr(ti)
	if err != nil {
		return out
	}
	for i := uint16(0); i < attr.CFuncs; i++ {
		var pfd *funcDescHead
		if hr := comCallHR(ti, 5, uintptr(i), uintptr(unsafe.Pointer(&pfd))); hr.Failed() || pfd == nil {
			continue
		}
		memid := pfd.Memid
		comCall(ti, 20, uintptr(unsafe.Pointer(pfd))) // ReleaseFuncDesc

		var nameB uintptr
		var got uint32
		if hr := comCallHR(ti, 7, uintptr(memid),
			uintptr(unsafe.Pointer(&nameB)), 1, uintptr(unsafe.Pointer(&got))); hr.Failed() || got == 0 {
			continue
		}
		name := bstrTake(nameB)
		if _, dup := out[memid]; !dup {
			out[memid] = name
		}
	}
	return out
}

// tiMemberID resolves a member name on an ITypeInfo.
func tiMemberID(ti uintptr, name string) (int32, HRESULT) {
	w := utf16Ptr(name)
	var id int32
	hr := comCallHR(ti, 10,
		uintptr(unsafe.Pointer(&w)), 1, uintptr(unsafe.Pointer(&id)))
	return id, hr
}

// tiInvoke calls a member of a plain (non-IDispatch) COM interface using its
// type information, which performs the argument marshalling for us.
func tiInvoke(ti uintptr, instance uintptr, memid int32, flags uint16, args ...interface{}) (VARIANT, error) {
	vars := make([]VARIANT, len(args))
	for i, a := range args {
		v, err := newVariant(a)
		if err != nil {
			for j := 0; j < i; j++ {
				releaseVariant(&vars[j])
			}
			return VARIANT{}, err
		}
		vars[len(args)-1-i] = v
	}
	defer func() {
		for i := range vars {
			releaseVariant(&vars[i])
		}
	}()

	var dp DISPPARAMS
	dp.CArgs = uint32(len(vars))
	if len(vars) > 0 {
		dp.Rgvarg = &vars[0]
	}
	putID := int32(DISPID_PROPERTYPUT)
	if flags&(DISPATCH_PROPERTYPUT|DISPATCH_PROPERTYPUTREF) != 0 {
		dp.RgdispidNamedArgs = &putID
		dp.CNamedArgs = 1
	}

	var result VARIANT
	var ex EXCEPINFO
	var argErr uint32
	hr := comCallHR(ti, 11,
		instance,
		uintptr(memid),
		uintptr(flags),
		uintptr(unsafe.Pointer(&dp)),
		uintptr(unsafe.Pointer(&result)),
		uintptr(unsafe.Pointer(&ex)),
		uintptr(unsafe.Pointer(&argErr)))
	if hr.Failed() {
		msg := ex.message()
		ex.free()
		if msg != "" {
			return VARIANT{}, fmt.Errorf("%s: %s", hr.Error(), msg)
		}
		return VARIANT{}, fmt.Errorf("%s", hr.Error())
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Coclass selection
// ---------------------------------------------------------------------------

// coclassInfo describes a candidate RDP client coclass found in mstscax.dll.
type coclassInfo struct {
	Type    *tlType
	Version int  // trailing version number: MsRdpClient11 -> 11, MsRdpClient -> 1
	NotSafe bool // "NotSafeForScripting" variant
	Rdp     bool // MsRdpClient* rather than MsTscAx*
}

func (c coclassInfo) String() string {
	return fmt.Sprintf("%s %s", c.Type.Name, c.Type.GUID)
}

// rdpCoclasses returns every Remote Desktop client coclass in the type library,
// best candidate first.
//
// Ranking: the "NotSafeForScripting" variants come first because they are the
// ones that accept a password through ClearTextPassword without prompting;
// within each group the newest interface revision wins.
func rdpCoclasses(tl *typeLib) []coclassInfo {
	var out []coclassInfo
	for _, t := range tl.Types {
		if t.Kind != TKIND_COCLASS {
			continue
		}
		name := t.Name
		isRdp := strings.HasPrefix(name, "MsRdpClient")
		isTsc := strings.HasPrefix(name, "MsTscAx")
		if !isRdp && !isTsc {
			continue
		}
		rest := strings.TrimPrefix(strings.TrimPrefix(name, "MsRdpClient"), "MsTscAx")
		notSafe := strings.HasSuffix(rest, "NotSafeForScripting")
		rest = strings.TrimSuffix(rest, "NotSafeForScripting")
		version := 1
		if rest != "" {
			n, err := strconv.Atoi(rest)
			if err != nil {
				continue // not a coclass we understand
			}
			version = n
		}
		out = append(out, coclassInfo{Type: t, Version: version, NotSafe: notSafe, Rdp: isRdp})
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.NotSafe != b.NotSafe {
			return a.NotSafe
		}
		if a.Rdp != b.Rdp {
			return a.Rdp
		}
		return a.Version > b.Version
	})
	return out
}

// nonScriptableInterfaces returns the IMsRdpClientNonScriptable* interfaces of
// the library, newest first. Those carry the members (password, multi-monitor,
// credential prompting) that are deliberately not reachable through IDispatch.
func nonScriptableInterfaces(tl *typeLib) []*tlType {
	var out []*tlType
	for _, t := range tl.Types {
		if t.Kind != TKIND_INTERFACE && t.Kind != TKIND_DISPATCH {
			continue
		}
		if !strings.HasPrefix(t.Name, "IMsRdpClientNonScriptable") &&
			!strings.HasPrefix(t.Name, "IMsTscNonScriptable") {
			continue
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return trailingVersion(out[i].Name) > trailingVersion(out[j].Name)
	})
	return out
}
