//go:build !windows

package main

import (
	"os"
	"syscall"
)

// The operator runs each resumed session in its own tmux window, so ccarchive
// does not come back.
func resume(t resumeTarget) error {
	if err := os.Chdir(t.cwd); err != nil {
		return err
	}
	return syscall.Exec(t.claude, []string{"claude", "--resume", t.uuid}, os.Environ())
}
