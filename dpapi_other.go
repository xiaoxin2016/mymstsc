//go:build !windows

package main

// decryptRDPPassword is a no-op off Windows: the "password 51" field of a .rdp
// file is a DPAPI blob that only the Windows Data Protection API can unwrap.
func decryptRDPPassword(hexBlob string) (string, error) {
	return "", errf("stored .rdp passwords can only be decrypted on Windows")
}
