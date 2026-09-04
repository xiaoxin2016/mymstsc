//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	modCrypt32         = syscall.NewLazyDLL("crypt32.dll")
	procCryptUnprotect = modCrypt32.NewProc("CryptUnprotectData")
	procLocalFree      = modKernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

// decryptRDPPassword unwraps the "password 51:b:" field of a .rdp file.
//
// mstsc stores that field as a DPAPI blob (CryptProtectData) of the UTF-16
// password, bound to the user account that saved the file. Decryption therefore
// only succeeds when this program runs as that same user on that same machine,
// which is exactly the protection boundary mstsc itself relies on.
func decryptRDPPassword(hexBlob string) (string, error) {
	raw, err := hexDecode(hexBlob)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", errf("empty password blob")
	}
	in := dataBlob{cbData: uint32(len(raw)), pbData: &raw[0]}
	var out dataBlob
	r, _, e := procCryptUnprotect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // ppszDataDescr
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		0, // dwFlags
		uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return "", errf("CryptUnprotectData failed: %v", e)
	}
	defer func() {
		if out.pbData != nil {
			procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
		}
	}()
	if out.cbData == 0 || out.pbData == nil {
		return "", errf("CryptUnprotectData returned no data")
	}
	buf := unsafe.Slice(out.pbData, int(out.cbData))
	pw := utf16BytesToString(buf)
	// Wipe the plaintext copy the API handed back before releasing it.
	for i := range buf {
		buf[i] = 0
	}
	return pw, nil
}
