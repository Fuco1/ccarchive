package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// os.Rename passes MOVEFILE_REPLACE_EXISTING; without it MoveFileEx refuses an
// existing destination, and without MOVEFILE_COPY_ALLOWED it never copies.
func renameNoReplace(src, dst string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, 0); err != nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
	}
	return nil
}
