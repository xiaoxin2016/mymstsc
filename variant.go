//go:build windows

package main

import (
	"fmt"
	"unsafe"
)

// VARIANT type tags (wtypes.h).
const (
	VT_EMPTY    = 0
	VT_NULL     = 1
	VT_I2       = 2
	VT_I4       = 3
	VT_R4       = 4
	VT_R8       = 5
	VT_DATE     = 7
	VT_BSTR     = 8
	VT_DISPATCH = 9
	VT_ERROR    = 10
	VT_BOOL     = 11
	VT_VARIANT  = 12
	VT_UNKNOWN  = 13
	VT_UI1      = 17
	VT_UI2      = 18
	VT_UI4      = 19
	VT_I8       = 20
	VT_UI8      = 21
	VT_INT      = 22
	VT_UINT     = 23
	VT_BYREF    = 0x4000
)

// IDispatch::Invoke flags.
const (
	DISPATCH_METHOD         = 0x1
	DISPATCH_PROPERTYGET    = 0x2
	DISPATCH_PROPERTYPUT    = 0x4
	DISPATCH_PROPERTYPUTREF = 0x8
)

const DISPID_PROPERTYPUT = -3

// VARIANT mirrors the Win32 VARIANT. The two uintptr words give 16 bytes on
// 386 and 24 bytes on amd64/arm64, matching the platform ABI in both cases.
type VARIANT struct {
	VT   uint16
	_    uint16
	_    uint16
	_    uint16
	data [2]uintptr
}

func (v *VARIANT) i64() *int64   { return (*int64)(unsafe.Pointer(&v.data[0])) }
func (v *VARIANT) f64() *float64 { return (*float64)(unsafe.Pointer(&v.data[0])) }
func (v *VARIANT) i32() *int32   { return (*int32)(unsafe.Pointer(&v.data[0])) }
func (v *VARIANT) i16() *int16   { return (*int16)(unsafe.Pointer(&v.data[0])) }
func (v *VARIANT) ptr() uintptr  { return v.data[0] }
func (v *VARIANT) clear()        { procVariantClear.Call(uintptr(unsafe.Pointer(v))) }

// DISPPARAMS mirrors the Win32 DISPPARAMS.
type DISPPARAMS struct {
	Rgvarg            *VARIANT
	RgdispidNamedArgs *int32
	CArgs             uint32
	CNamedArgs        uint32
}

// EXCEPINFO mirrors the Win32 EXCEPINFO.
type EXCEPINFO struct {
	Code         uint16
	Reserved     uint16
	Source       uintptr // BSTR
	Description  uintptr // BSTR
	HelpFile     uintptr // BSTR
	HelpContext  uint32
	Reserved2    uintptr
	DeferredFill uintptr
	Scode        int32
}

func (e *EXCEPINFO) message() string {
	desc := bstrToString(e.Description)
	src := bstrToString(e.Source)
	switch {
	case desc != "" && src != "":
		return fmt.Sprintf("%s: %s", src, desc)
	case desc != "":
		return desc
	case src != "":
		return src
	case e.Scode != 0:
		return HRESULT(e.Scode).Error()
	case e.Code != 0:
		return fmt.Sprintf("exception code %d", e.Code)
	}
	return ""
}

func (e *EXCEPINFO) free() {
	sysFreeString(e.Source)
	sysFreeString(e.Description)
	sysFreeString(e.HelpFile)
	e.Source, e.Description, e.HelpFile = 0, 0, 0
}

// newVariant converts a Go value into a VARIANT. Values holding a BSTR must be
// released with releaseVariant once the call that consumes them has returned.
func newVariant(v interface{}) (VARIANT, error) {
	var out VARIANT
	switch t := v.(type) {
	case nil:
		out.VT = VT_EMPTY
	case string:
		b := sysAllocString(t)
		if b == 0 {
			return out, fmt.Errorf("SysAllocString failed")
		}
		out.VT = VT_BSTR
		out.data[0] = b
	case bool:
		out.VT = VT_BOOL
		if t {
			*out.i16() = -1
		} else {
			*out.i16() = 0
		}
	case int:
		out.VT = VT_I4
		*out.i32() = int32(t)
	case int32:
		out.VT = VT_I4
		*out.i32() = t
	case uint32:
		out.VT = VT_UI4
		*out.i32() = int32(t)
	case int64:
		out.VT = VT_I8
		*out.i64() = t
	case float64:
		out.VT = VT_R8
		*out.f64() = t
	case VARIANT:
		out = t
	default:
		return out, fmt.Errorf("unsupported argument type %T", v)
	}
	return out, nil
}

func releaseVariant(v *VARIANT) {
	if v.VT == VT_BSTR {
		sysFreeString(v.data[0])
		v.VT = VT_EMPTY
		v.data[0] = 0
	}
}

// Value converts a VARIANT into a plain Go value.
func (v *VARIANT) Value() interface{} {
	switch v.VT {
	case VT_EMPTY, VT_NULL:
		return nil
	case VT_I2:
		return int32(*v.i16())
	case VT_I4, VT_INT, VT_ERROR:
		return *v.i32()
	case VT_UI4, VT_UINT:
		return int32(*v.i32())
	case VT_UI1:
		return int32(v.data[0] & 0xFF)
	case VT_UI2:
		return int32(v.data[0] & 0xFFFF)
	case VT_I8, VT_UI8:
		return *v.i64()
	case VT_R8:
		return *v.f64()
	case VT_R4:
		return float64(*(*float32)(unsafe.Pointer(&v.data[0])))
	case VT_BOOL:
		return *v.i16() != 0
	case VT_BSTR:
		return bstrToString(v.data[0])
	case VT_DISPATCH, VT_UNKNOWN:
		return v.data[0]
	}
	if v.VT&VT_BYREF != 0 && v.data[0] != 0 {
		// A by-reference VARIANT stores a raw pointer supplied by the caller.
		inner := (*VARIANT)(unsafe.Pointer(v.data[0]))
		if v.VT == (VT_VARIANT | VT_BYREF) {
			return inner.Value()
		}
	}
	return fmt.Sprintf("<VARIANT vt=%d>", v.VT)
}

// Int coerces the VARIANT to an int32 where that is meaningful.
func (v *VARIANT) Int() (int32, bool) {
	switch x := v.Value().(type) {
	case int32:
		return x, true
	case int64:
		return int32(x), true
	case float64:
		return int32(x), true
	case bool:
		if x {
			return -1, true
		}
		return 0, true
	}
	return 0, false
}

// Bool coerces the VARIANT to a bool where that is meaningful.
func (v *VARIANT) Bool() (bool, bool) {
	if n, ok := v.Int(); ok {
		return n != 0, true
	}
	return false, false
}

// Str renders the VARIANT as a string for logging.
func (v *VARIANT) Str() string {
	return fmt.Sprintf("%v", v.Value())
}
