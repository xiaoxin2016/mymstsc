//go:build !windows

package main

import (
	"fmt"
	"os"
)

// reportFatal prints an error. The message-box fallback exists only on Windows.
func reportFatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
}
