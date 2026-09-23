package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Run as a child by the tests below: it exits 3 only when started in the
// directory it was told to expect, so a dropped Dir shows as 4.
func TestHelperChild(t *testing.T) {
	want := os.Getenv("CCARCHIVE_HELPER_DIR")
	if want == "" {
		t.Skip("run only as a child")
	}
	wd, _ := os.Getwd()
	wd, _ = filepath.EvalSymlinks(wd)
	want, _ = filepath.EvalSymlinks(want)
	if wd != want {
		os.Exit(4)
	}
	os.Exit(3)
}

func TestRunChildReturnsChildExitCodeFromGivenDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CCARCHIVE_HELPER_DIR", dir)
	code, err := runChild(dir, os.Args[0], "-test.run=^TestHelperChild$")
	if err != nil || code != 3 {
		t.Fatalf("runChild = %d, %v; want 3, nil", code, err)
	}
}

func TestRunChildThatFailsToStartReturnsError(t *testing.T) {
	if _, err := runChild(t.TempDir(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("no error for a child that cannot start")
	}
}
