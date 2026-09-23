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

// CreateProcess caps a Windows command line at this many UTF-16 units, far
// below ARG_MAX elsewhere, so one bound serves every platform.
const maxCmdLine = 32767

// rg exits 1 when nothing matched, and 2 when some file could not be read, in
// which case what it printed still matched. A command line past maxCmdLine
// would not start, so that search is scanned in process instead.
func rgSearch(rg, query string, paths []string) ([]string, error) {
	// rg given no paths searches its working directory.
	if len(paths) == 0 {
		return nil, nil
	}
	args := append([]string{"--files-with-matches", "--fixed-strings", "--ignore-case", "--", query}, paths...)
	if cmdLineLen(rg, query, args) > maxCmdLine {
		return scanSearch(query, paths)
	}
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

// An upper bound on syscall.EscapeArg's output: an argument gains at most a
// separator and two quotes, and only the query can hold quotes, each escape
// at most doubling it. A UTF-8 byte count is never below its UTF-16 length.
func cmdLineLen(rg, query string, args []string) int {
	n := len(rg) + 3 + len(query)
	for _, a := range args {
		n += len(a) + 3
	}
	return n
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
