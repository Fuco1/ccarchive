package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// rename is os.Rename; tests replace it to make one move of a pair fail.
var rename = os.Rename

// setArchived moves s between <config>/projects and <data>/archive. There is no
// copy fallback when the two sit on different filesystems: a copy-then-delete
// that dies halfway is where a transcript gets lost.
func setArchived(s Session, archived bool, config, data string) (Session, error) {
	if s.Archived == archived {
		return s, nil
	}
	from, to := activeRoot(config), archiveRoot(data)
	if !archived {
		from, to = to, from
	}
	from, to = filepath.Join(from, s.Project), filepath.Join(to, s.Project)

	if live, err := isLive(config, s.UUID); err != nil {
		return s, err
	} else if live {
		return s, fmt.Errorf("session %s is running; quit it first", s.UUID)
	}
	names := []string{s.UUID + ".jsonl"}
	if _, err := os.Lstat(filepath.Join(from, s.UUID)); err == nil {
		names = append(names, s.UUID)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return s, err
	}
	// os.Rename replaces an existing file on Linux and Windows, and an empty
	// directory on Linux.
	// ponytail: stat-then-rename races a concurrent writer of the destination;
	// renameat2(RENAME_NOREPLACE) closes it on Linux only.
	for _, n := range names {
		if _, err := os.Lstat(filepath.Join(to, n)); err == nil {
			return s, fmt.Errorf("%s already exists", filepath.Join(to, n))
		} else if !errors.Is(err, fs.ErrNotExist) {
			return s, err
		}
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return s, err
	}
	for i, n := range names {
		if err := rename(filepath.Join(from, n), filepath.Join(to, n)); err != nil {
			for _, done := range names[:i] {
				if uerr := rename(filepath.Join(to, done), filepath.Join(from, done)); uerr != nil {
					return s, fmt.Errorf("%w; moving %s back also failed: %v", err, done, uerr)
				}
			}
			return s, err
		}
	}
	s.Archived = archived
	s.Path = filepath.Join(to, names[0])
	return s, nil
}

// isLive reports whether a <config>/sessions/*.json names uuid with a running
// pid. A reused pid makes it refuse a session that is not live, which is safe.
func isLive(config, uuid string) (bool, error) {
	files, err := filepath.Glob(filepath.Join(config, "sessions", "*.json"))
	if err != nil {
		return false, err
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return false, err
		}
		var rec struct {
			PID       int    `json:"pid"`
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(b, &rec) != nil || rec.SessionID != uuid {
			continue
		}
		if err := syscall.Kill(rec.PID, 0); err == nil || errors.Is(err, syscall.EPERM) {
			return true, nil
		}
	}
	return false, nil
}
