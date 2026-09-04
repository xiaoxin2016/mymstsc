//go:build !windows

package main

import (
	"bufio"
	"os"
	"strings"
)

// readSecret reads one line from standard input. Echo suppression is only
// implemented for the Windows console.
func readSecret(prompt string) (string, error) {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", errf("reading password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
