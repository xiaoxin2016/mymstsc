//go:build !windows

// Package main builds only on Windows: it hosts the Remote Desktop ActiveX
// control that is part of the operating system. This stub keeps "go vet" and
// "go build ./..." usable on other platforms.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "mymstsc runs on Windows only; "+
		"build it with GOOS=windows GOARCH=amd64 go build")
	os.Exit(1)
}
