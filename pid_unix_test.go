//go:build !windows

package main

import (
	"strings"
	"syscall"
	"testing"
)

func TestArchiveRefusesSessionWhosePidOnlyAnswersEPERM(t *testing.T) {
	if err := syscall.Kill(1, 0); err != syscall.EPERM {
		t.Skipf("kill(1, 0) = %v, want EPERM; run as a non-root user", err)
	}
	st, m := newStore(t)
	writeSessionFile(t, st, "1.json", uuidA, 1)
	m, _ = press(t, m, "a")
	if m.err == nil || !strings.Contains(m.View(), "is running") {
		t.Fatalf("session of another user's process not refused:\n%s", m.View())
	}
}
