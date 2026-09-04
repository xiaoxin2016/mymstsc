package main

import "fmt"

// ---------------------------------------------------------------------------
// HRESULT
// ---------------------------------------------------------------------------

type HRESULT int32

// Failure codes are written as their documented 32-bit value minus 2^32, so
// the hexadecimal constant from winerror.h stays visible and the signed value
// is derived by the compiler rather than by hand.
const (
	S_OK    HRESULT = 0
	S_FALSE HRESULT = 1

	E_NOTIMPL     HRESULT = 0x80004001 - 1<<32
	E_NOINTERFACE HRESULT = 0x80004002 - 1<<32
	E_POINTER     HRESULT = 0x80004003 - 1<<32
	E_FAIL        HRESULT = 0x80004005 - 1<<32
	E_OUTOFMEMORY HRESULT = 0x8007000E - 1<<32
	E_UNEXPECTED  HRESULT = 0x8000FFFF - 1<<32
	E_INVALIDARG  HRESULT = 0x80070057 - 1<<32

	DISP_E_MEMBERNOTFOUND HRESULT = 0x80020003 - 1<<32
	DISP_E_UNKNOWNNAME    HRESULT = 0x80020006 - 1<<32
	DISP_E_EXCEPTION      HRESULT = 0x80020009 - 1<<32
	DISP_E_BADPARAMCOUNT  HRESULT = 0x8002000E - 1<<32

	REGDB_E_CLASSNOTREG       HRESULT = 0x80040154 - 1<<32
	CLASS_E_CLASSNOTAVAILABLE HRESULT = 0x80040111 - 1<<32
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
	REGDB_E_CLASSNOTREG:       "REGDB_E_CLASSNOTREG",
	CLASS_E_CLASSNOTAVAILABLE: "CLASS_E_CLASSNOTAVAILABLE",
}
