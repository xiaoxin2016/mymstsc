package main

import (
	"fmt"
	"strconv"
)

// versionedNames yields "Name<max>", ... "Name2", "Name" so that the newest
// interface revision the host provides is used.
func versionedNames(base string, max int) []string {
	out := make([]string, 0, max)
	for i := max; i >= 2; i-- {
		out = append(out, fmt.Sprintf("%s%d", base, i))
	}
	return append(out, base)
}

func trailingVersion(name string) int {
	i := len(name)
	for i > 0 && name[i-1] >= '0' && name[i-1] <= '9' {
		i--
	}
	if i == len(name) {
		return 1
	}
	n, err := strconv.Atoi(name[i:])
	if err != nil {
		return 1
	}
	return n
}
