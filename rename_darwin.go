package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// os.Rename replaces an existing file or empty directory, and checking the
// destination first races whoever creates it in between.
func renameNoReplace(src, dst string) error {
	if err := unix.RenamexNp(src, dst, unix.RENAME_EXCL); err != nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
	}
	return nil
}
