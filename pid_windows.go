package main

import (
	"errors"

	"golang.org/x/sys/windows"
)

// golang.org/x/sys/windows does not export it.
const STILL_ACTIVE = 259

// Windows has no signal 0. Access denied means the process exists under
// another user, and an unreadable exit code counts as live because a false
// refusal is the safe direction.
func pidRunning(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return true
	}
	return code == STILL_ACTIVE
}
