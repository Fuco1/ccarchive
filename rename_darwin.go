package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// os.Rename replaces an existing file, and its own check for an existing
// directory is an Lstat that races whoever creates one in between.
func renameNoReplace(src, dst string) error {
	if err := unix.RenamexNp(src, dst, unix.RENAME_EXCL); err != nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
	}
	return nil
}
