package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// One name, so the checked binary and the exec'd argv[0] cannot drift apart
// when Claude's invocation changes.
const claudeBin = "claude"

type resumeTarget struct {
	claude string
	cwd    string
	uuid   string
}

// prepareResume checks what the exec needs while the TUI can still show why
// it cannot happen.
func prepareResume(s Session) (resumeTarget, error) {
	if s.Cwd == "" {
		return resumeTarget{}, fmt.Errorf("session %s records no cwd", s.UUID)
	}
	if fi, err := os.Stat(s.Cwd); err != nil {
		return resumeTarget{}, err
	} else if !fi.IsDir() {
		return resumeTarget{}, fmt.Errorf("%s is not a directory", s.Cwd)
	}
	claude, err := exec.LookPath(claudeBin)
	if err != nil {
		return resumeTarget{}, err
	}
	return resumeTarget{claude: claude, cwd: s.Cwd, uuid: s.UUID}, nil
}

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
