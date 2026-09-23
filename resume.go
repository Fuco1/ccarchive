package main

import (
	"errors"
	"os"
	"os/exec"
)

// runChild is portable os/exec, so it carries no build constraint and its test
// runs on any host; only the Windows resume path calls it.
func runChild(dir, name string, args ...string) (int, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return 0, err
}
