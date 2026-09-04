//go:build windows

package main

import (
	"testing"
	"unsafe"
)

// The structures below are passed straight to Win32 and OLE, so their layout
// must match the platform ABI exactly. The expected sizes are those of the
// corresponding C structures on x64.
func TestStructLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("expected sizes are those of the x64 ABI")
	}
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"GUID", unsafe.Sizeof(GUID{}), 16},
		{"RECT", unsafe.Sizeof(RECT{}), 16},
		{"MSG", unsafe.Sizeof(MSG{}), 48},
		{"WNDCLASSEXW", unsafe.Sizeof(WNDCLASSEX{}), 80},
		{"VARIANT", unsafe.Sizeof(VARIANT{}), 24},
		{"DISPPARAMS", unsafe.Sizeof(DISPPARAMS{}), 24},
		{"EXCEPINFO", unsafe.Sizeof(EXCEPINFO{}), 64},
		{"OLEINPLACEFRAMEINFO", unsafe.Sizeof(OLEINPLACEFRAMEINFO{}), 32},
		{"TYPEDESC", unsafe.Sizeof(TYPEDESC{}), 16},
		{"IDLDESC", unsafe.Sizeof(IDLDESC{}), 16},
		{"TYPEATTR", unsafe.Sizeof(TYPEATTR{}), 96},
		{"DATA_BLOB", unsafe.Sizeof(dataBlob{}), 16},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("sizeof(%s) = %d; the C structure is %d bytes", c.name, c.got, c.want)
		}
	}

	offsets := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"MSG.WParam", unsafe.Offsetof(MSG{}.WParam), 16},
		{"MSG.Pt", unsafe.Offsetof(MSG{}.Pt), 36},
		{"WNDCLASSEX.ClassName", unsafe.Offsetof(WNDCLASSEX{}.ClassName), 64},
		{"EXCEPINFO.Scode", unsafe.Offsetof(EXCEPINFO{}.Scode), 56},
		{"TYPEATTR.Typekind", unsafe.Offsetof(TYPEATTR{}.Typekind), 44},
		{"TYPEATTR.CImplTypes", unsafe.Offsetof(TYPEATTR{}.CImplTypes), 52},
	}
	for _, c := range offsets {
		if c.got != c.want {
			t.Errorf("offsetof(%s) = %d; want %d", c.name, c.got, c.want)
		}
	}
}

// A vtable with the wrong number of slots corrupts every call past the missing
// one, so the counts are pinned here.
func TestVtableSlotCounts(t *testing.T) {
	cases := []struct {
		name string
		vtbl []uintptr
		want int
	}{
		{"IOleClientSite", vtblOleClientSite, 9},
		{"IOleInPlaceSite", vtblOleInPlaceSite, 15},
		{"IOleInPlaceFrame", vtblOleInPlaceFrame, 15},
		{"IOleControlSite", vtblOleControlSite, 10},
		{"IDispatch (ambient)", vtblAmbientDispatch, 7},
		{"IDispatch (event sink)", vtblEventSink, 7},
	}
	for _, c := range cases {
		if len(c.vtbl) != c.want {
			t.Errorf("%s vtable has %d slots; want %d", c.name, len(c.vtbl), c.want)
		}
		for i, fn := range c.vtbl {
			if fn == 0 {
				t.Errorf("%s vtable slot %d is nil", c.name, i)
			}
		}
	}
}

func TestGUIDParsing(t *testing.T) {
	// A well-known IID, checked field by field.
	g := mustGUID("{00020400-0000-0000-C000-000000000046}")
	if g.Data1 != 0x00020400 || g.Data2 != 0 || g.Data3 != 0 {
		t.Fatalf("parsed %s", g)
	}
	if g.Data4 != [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46} {
		t.Fatalf("Data4 = % x", g.Data4)
	}
	if !g.Equals(&IID_IDispatch) {
		t.Error("should equal IID_IDispatch")
	}
	if g.Equals(&IID_IUnknown) {
		t.Error("should not equal IID_IUnknown")
	}
	if got := g.String(); got != "{00020400-0000-0000-C000-000000000046}" {
		t.Errorf("String() = %s", got)
	}
}

func TestHRESULTConstants(t *testing.T) {
	// The negative literals must correspond to the documented hex values.
	cases := []struct {
		hr   HRESULT
		want uint32
	}{
		{E_NOTIMPL, 0x80004001},
		{E_NOINTERFACE, 0x80004002},
		{E_POINTER, 0x80004003},
		{E_FAIL, 0x80004005},
		{E_OUTOFMEMORY, 0x8007000E},
		{E_UNEXPECTED, 0x8000FFFF},
		{E_INVALIDARG, 0x80070057},
		{DISP_E_MEMBERNOTFOUND, 0x80020003},
		{DISP_E_UNKNOWNNAME, 0x80020006},
		{DISP_E_EXCEPTION, 0x80020009},
		{REGDB_E_CLASSNOTREG, 0x80040154},
		{TYPE_E_ELEMENTNOTFOUND, 0x8002802B},
	}
	for _, c := range cases {
		if uint32(c.hr) != c.want {
			t.Errorf("HRESULT %d is 0x%08X; want 0x%08X", c.hr, uint32(c.hr), c.want)
		}
		if !c.hr.Failed() {
			t.Errorf("0x%08X should be a failure code", c.want)
		}
	}
	if S_OK.Failed() || S_FALSE.Failed() {
		t.Error("S_OK and S_FALSE are success codes")
	}
	if hres(E_NOTIMPL) != uintptr(uint32(0x80004001)) {
		t.Error("hres must widen the HRESULT without sign extension in the low word")
	}
}

func TestVariantRoundTrip(t *testing.T) {
	v, err := newVariant(int32(-42))
	if err != nil {
		t.Fatal(err)
	}
	if v.VT != VT_I4 {
		t.Fatalf("vt = %d", v.VT)
	}
	if n, ok := v.Int(); !ok || n != -42 {
		t.Errorf("Int() = %d, %v", n, ok)
	}

	v, err = newVariant(true)
	if err != nil {
		t.Fatal(err)
	}
	if v.VT != VT_BOOL {
		t.Fatalf("vt = %d", v.VT)
	}
	if b, ok := v.Bool(); !ok || !b {
		t.Errorf("Bool() = %v, %v", b, ok)
	}

	v, err = newVariant("hello")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseVariant(&v)
	if v.VT != VT_BSTR {
		t.Fatalf("vt = %d", v.VT)
	}
	if s, ok := v.Value().(string); !ok || s != "hello" {
		t.Errorf("Value() = %#v", v.Value())
	}

	if _, err := newVariant(struct{}{}); err == nil {
		t.Error("an unsupported type should be rejected")
	}
}
