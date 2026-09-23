package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const trashRetention = 30 * 24 * time.Hour

const (
	originActive  = "active"
	originArchive = "archive"
)

// A move keeps the transcript's mtime, so the deletion time lives here.
type sidecar struct {
	DeletedAt time.Time `json:"deletedAt"`
	Origin    string    `json:"origin"`
}

func sidecarPath(dir, uuid string) string { return filepath.Join(dir, uuid+".trashed") }

// readSidecar returns a zero time and the active store for a sidecar that is
// missing, malformed or names an unknown origin: that entry's deletion time is
// unknown, so the startup purge leaves it alone.
func readSidecar(path string) (deletedAt time.Time, archived bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false
	}
	var sc sidecar
	if json.Unmarshal(b, &sc) != nil || sc.DeletedAt.IsZero() ||
		sc.Origin != originActive && sc.Origin != originArchive {
		return time.Time{}, false
	}
	return sc.DeletedAt, sc.Origin == originArchive
}

func trash(s Session, config, data string) (Session, error) {
	from, origin := activeRoot(config), originActive
	if s.Archived {
		from, origin = archiveRoot(data), originArchive
	}
	moved, err := moveSession(s, from, trashRoot(data), config)
	if err != nil {
		return s, err
	}
	now := time.Now()
	// A sidecar-less entry would be restored to the active store and never
	// purged, so failing to write one undoes the move.
	if err := writeSidecar(sidecarPath(filepath.Dir(moved.Path), s.UUID), sidecar{now, origin}); err != nil {
		if _, uerr := moveSession(moved, trashRoot(data), from, config); uerr != nil {
			return s, fmt.Errorf("%w; moving the session back also failed: %v", err, uerr)
		}
		return s, err
	}
	moved.Trashed, moved.TrashedAt = true, now
	return moved, nil
}

func writeSidecar(path string, sc sidecar) error {
	b, err := json.Marshal(sc)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

// restore returns the moved session with a non-nil error when only the
// sidecar's removal failed, so the caller records where the session now is.
func restore(s Session, config, data string) (Session, error) {
	to := activeRoot(config)
	if s.Archived {
		to = archiveRoot(data)
	}
	moved, err := moveSession(s, trashRoot(data), to, config)
	if err != nil {
		return s, err
	}
	moved.Trashed, moved.TrashedAt = false, time.Time{}
	if err := os.Remove(sidecarPath(filepath.Dir(s.Path), s.UUID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return moved, err
	}
	return moved, nil
}

// purge validates the uuid before building any path from it: an empty one
// turns <config>/file-history/<uuid> into every session's history.
func purge(s Session, config, data string) error {
	if !canonicalUUID.MatchString(s.UUID) {
		return fmt.Errorf("refusing to purge %q: not a canonical UUID", s.UUID)
	}
	if !s.Trashed {
		return fmt.Errorf("refusing to purge %s: not in the trash", s.UUID)
	}
	dir := filepath.Join(trashRoot(data), s.Project)
	// The transcript and then the sidecar go last, so a purge that fails
	// earlier still lists the entry with its deletion time for a retry.
	for _, p := range []string{
		filepath.Join(config, "file-history", s.UUID),
		filepath.Join(config, "session-env", s.UUID),
		filepath.Join(dir, s.UUID),
		filepath.Join(dir, s.UUID+".jsonl"),
		sidecarPath(dir, s.UUID),
	} {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}

// purgeExpired returns the sessions it did not purge. An entry with an
// unreadable sidecar has a zero TrashedAt and is never purged here.
func purgeExpired(sessions []Session, config, data string, now time.Time) ([]Session, error) {
	var kept []Session
	var errs []error
	for _, s := range sessions {
		if s.Trashed && !s.TrashedAt.IsZero() && now.Sub(s.TrashedAt) > trashRetention {
			err := purge(s, config, data)
			if err == nil {
				continue
			}
			errs = append(errs, err)
		}
		kept = append(kept, s)
	}
	return kept, errors.Join(errs...)
}
