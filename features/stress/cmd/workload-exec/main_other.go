//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "catmonitor-stress-exec: Linux is required")
	os.Exit(1)
}
