package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

func matchesFilter(s Session, lowerQuery string) bool {
	for _, f := range []string{s.Title, s.Cwd, s.Prompt} {
		if strings.Contains(strings.ToLower(f), lowerQuery) {
			return true
		}
	}
	return false
}

// rg exits 1 when nothing matched, and 2 when some file could not be read, in
// which case what it printed still matched.
// ponytail: every path is one argv entry, so a view past ARG_MAX (~2 MiB on
// Linux, 32 KiB on Windows) fails; batch the paths if that is ever reached.
func rgSearch(rg, query string, paths []string) ([]string, error) {
	// rg given no paths searches its working directory.
	if len(paths) == 0 {
		return nil, nil
	}
	args := append([]string{"--files-with-matches", "--fixed-strings", "--ignore-case", "--", query}, paths...)
	cmd := exec.Command(rg, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var printed []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSuffix(l, "\r"); l != "" {
			printed = append(printed, l)
		}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case 1:
			return nil, nil
		case 2:
			return printed, errors.New(strings.TrimSpace(stderr.String()))
		}
	}
	if err != nil {
		return nil, err
	}
	return printed, nil
}

// An unreadable file is reported and skipped rather than ending the scan, to
// behave as rg's exit status 2 does.
func scanSearch(query string, paths []string) ([]string, error) {
	q := bytes.ToLower([]byte(query))
	var out []string
	var errs []error
	for _, p := range paths {
		ok, err := fileContains(p, q)
		if err != nil {
			errs = append(errs, err)
		} else if ok {
			out = append(out, p)
		}
	}
	return out, errors.Join(errs...)
}

// ReadBytes rather than a bufio.Scanner, whose token limit would drop the
// long lines inlined attachments make.
func fileContains(path string, lowerQuery []byte) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if bytes.Contains(bytes.ToLower(line), lowerQuery) {
			return true, nil
		}
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
}
