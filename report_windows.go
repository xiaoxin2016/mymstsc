//go:build windows

package main

import (
	"fmt"
	"os"
)

// reportFatal shows an error on stderr, and additionally in a message box when
// the process has no console (built with -H windowsgui, or launched from a
// shortcut).
func reportFatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
	if !hasConsole() {
		messageBox(0, err.Error(), appName, MB_OK|MB_ICONERROR)
	}
}
