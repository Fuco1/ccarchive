package main

import (
	"errors"
	"os"
	"syscall"
)

// Windows has no signal 0; os.FindProcess opens a handle, which fails once the
// process is gone, and access denied means it exists under another user.
func pidRunning(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	p.Release()
	return true
}
