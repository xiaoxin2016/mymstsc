//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unsafe"
)

var (
	procGetStdHandle   = modKernel32.NewProc("GetStdHandle")
	procGetConsoleMode = modKernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = modKernel32.NewProc("SetConsoleMode")
)

const (
	stdInputHandle  = ^uintptr(9) // (DWORD)-10
	enableEchoInput = 0x0004
)

// readSecret reads one line from the console with echo turned off. When stdin
// is a pipe rather than a console, the line is simply read as-is.
func readSecret(prompt string) (string, error) {
	h, _, _ := procGetStdHandle.Call(stdInputHandle)
	var mode uint32
	isConsole := false
	if h != 0 && h != ^uintptr(0) {
		if r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode))); r != 0 {
			isConsole = true
		}
	}
	if isConsole {
		fmt.Fprint(os.Stderr, prompt)
		procSetConsoleMode.Call(h, uintptr(mode&^enableEchoInput))
		defer func() {
			procSetConsoleMode.Call(h, uintptr(mode))
			fmt.Fprintln(os.Stderr)
		}()
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", errf("reading password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
