package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const project = "-home-work"

type store struct{ cfg, data string }

func (st store) active(name string) string { return filepath.Join(st.cfg, "projects", project, name) }
func (st store) archived(name string) string {
	return filepath.Join(st.data, "archive", project, name)
}

// newStore holds one active session uuidA with a sibling directory, and returns
// the model listing it.
func newStore(t *testing.T) (store, model) {
	t.Helper()
	st := store{cfg: t.TempDir(), data: t.TempDir()}
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	write(t, st.active(uuidA+".jsonl"), `{"type":"user","cwd":"/w","message":{"content":"hello"}}`+"\n", old)
	write(t, st.active(filepath.Join(uuidA, "sub", "f.txt")), "sibling", old)
	return st, st.model(t)
}

func (st store) model(t *testing.T) model {
	t.Helper()
	sessions, err := listSessions(st.cfg, st.data)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := newModel(st.cfg, st.data, sessions).Update(sizeMsg)
	return m.(model)
}

type snapshot struct {
	content []byte
	mtime   time.Time
}

func snap(t *testing.T, path string) snapshot {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot{b, fi.ModTime()}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func mustExist(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if !exists(p) {
			t.Errorf("%s is missing", p)
		}
	}
}

func mustNotExist(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if exists(p) {
			t.Errorf("%s exists", p)
		}
	}
}

func TestArchiveMovesJsonlAndSiblingDirIntoArchive(t *testing.T) {
	st, m := newStore(t)
	m, _ = press(t, m, "a")
	if m.err != nil {
		t.Fatal(m.err)
	}
	mustExist(t, st.archived(uuidA+".jsonl"), st.archived(filepath.Join(uuidA, "sub", "f.txt")))
	mustNotExist(t, st.active(uuidA+".jsonl"), st.active(uuidA))
}

func TestArchiveWithoutSiblingDirMovesJsonl(t *testing.T) {
	st, _ := newStore(t)
	if err := os.RemoveAll(st.active(uuidA)); err != nil {
		t.Fatal(err)
	}
	m, _ := press(t, st.model(t), "a")
	if m.err != nil {
		t.Fatal(m.err)
	}
	mustExist(t, st.archived(uuidA+".jsonl"))
	mustNotExist(t, st.active(uuidA+".jsonl"), st.archived(uuidA))
}

func TestArchiveThenUnarchiveRestoresFilesByteIdenticalWithMtimes(t *testing.T) {
	st, m := newStore(t)
	files := []string{st.active(uuidA + ".jsonl"), st.active(filepath.Join(uuidA, "sub", "f.txt"))}
	before := make([]snapshot, len(files))
	for i, f := range files {
		before[i] = snap(t, f)
	}
	m, _ = press(t, m, "a")
	m, _ = press(t, m, "tab")
	m, _ = press(t, m, "a")
	if m.err != nil {
		t.Fatal(m.err)
	}
	for i, f := range files {
		after := snap(t, f)
		if !bytes.Equal(after.content, before[i].content) {
			t.Errorf("%s content changed", f)
		}
		if !after.mtime.Equal(before[i].mtime) {
			t.Errorf("%s mtime %v, was %v", f, after.mtime, before[i].mtime)
		}
	}
	mustNotExist(t, st.archived(uuidA+".jsonl"), st.archived(uuidA))
}

func TestUnarchiveCreatesAbsentProjectDir(t *testing.T) {
	st, m := newStore(t)
	m, _ = press(t, m, "a")
	if err := os.Remove(filepath.Dir(st.active(uuidA + ".jsonl"))); err != nil {
		t.Fatal(err)
	}
	m, _ = press(t, m, "tab")
	m, _ = press(t, m, "a")
	if m.err != nil {
		t.Fatal(m.err)
	}
	mustExist(t, st.active(uuidA+".jsonl"), st.active(filepath.Join(uuidA, "sub", "f.txt")))
}

func TestMoveRefusesExistingDestination(t *testing.T) {
	for _, tc := range []struct {
		name    string
		archive bool
		blocker string
		file    bool
	}{
		{"archive onto jsonl", true, uuidA + ".jsonl", true},
		// An empty directory is what os.Rename silently replaces on Linux.
		{"archive onto empty dir", true, uuidA, false},
		{"archive onto a file named like the dir", true, uuidA, true},
		{"unarchive onto jsonl", false, uuidA + ".jsonl", true},
		{"unarchive onto empty dir", false, uuidA, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, m := newStore(t)
			src, dst := st.active, st.archived
			if !tc.archive {
				m, _ = press(t, m, "a")
				m, _ = press(t, m, "tab")
				src, dst = st.archived, st.active
			}
			blocker := dst(tc.blocker)
			if tc.file {
				write(t, blocker, "someone else's", time.Now())
			} else if err := os.MkdirAll(blocker, 0o755); err != nil {
				t.Fatal(err)
			}
			m, _ = press(t, m, "a")
			if m.err == nil || !strings.Contains(m.View(), "already exists") {
				t.Fatalf("not refused:\n%s", m.View())
			}
			mustExist(t, src(uuidA+".jsonl"), src(filepath.Join(uuidA, "sub", "f.txt")))
			if tc.blocker != uuidA+".jsonl" {
				mustNotExist(t, dst(uuidA+".jsonl"))
			}
		})
	}
}

func failRename(t *testing.T, fails func(src string) bool, err error) {
	t.Helper()
	t.Cleanup(func() { rename = os.Rename })
	rename = func(src, dst string) error {
		if fails(src) {
			return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
		}
		return os.Rename(src, dst)
	}
}

func TestSiblingDirMoveFailureMovesJsonlBack(t *testing.T) {
	st, m := newStore(t)
	failRename(t, func(src string) bool { return filepath.Base(src) == uuidA }, syscall.EACCES)
	m, _ = press(t, m, "a")
	if m.err == nil || !strings.Contains(m.View(), "permission denied") {
		t.Fatalf("failure not shown:\n%s", m.View())
	}
	mustExist(t, st.active(uuidA+".jsonl"), st.active(filepath.Join(uuidA, "sub", "f.txt")))
	mustNotExist(t, st.archived(uuidA+".jsonl"), st.archived(uuidA))
	if m.sessions[0].Archived {
		t.Error("model records the session as archived")
	}
}

func TestCrossDeviceRenameLeavesSourceAndShowsError(t *testing.T) {
	st, m := newStore(t)
	failRename(t, func(string) bool { return true }, syscall.EXDEV)
	m, _ = press(t, m, "a")
	if !strings.Contains(m.View(), syscall.EXDEV.Error()) {
		t.Fatalf("EXDEV not shown:\n%s", m.View())
	}
	mustExist(t, st.active(uuidA+".jsonl"), st.active(filepath.Join(uuidA, "sub", "f.txt")))
	mustNotExist(t, st.archived(uuidA+".jsonl"), st.archived(uuidA))
}

func writeSessionFile(t *testing.T, st store, name, uuid string, pid int) {
	t.Helper()
	write(t, filepath.Join(st.cfg, "sessions", name),
		fmt.Sprintf(`{"pid":%d,"sessionId":%q,"cwd":"/w"}`, pid, uuid), time.Now())
}

func deadPID(t *testing.T) int {
	t.Helper()
	c := exec.Command("true")
	if err := c.Run(); err != nil {
		t.Skip(err)
	}
	return c.Process.Pid
}

func TestArchiveRefusesLiveSession(t *testing.T) {
	st, m := newStore(t)
	writeSessionFile(t, st, "1.json", uuidA, os.Getpid())
	m, _ = press(t, m, "a")
	if m.err == nil || !strings.Contains(m.View(), "is running") {
		t.Fatalf("live session not refused:\n%s", m.View())
	}
	mustExist(t, st.active(uuidA+".jsonl"))
	mustNotExist(t, st.archived(uuidA+".jsonl"))
}

func TestArchiveRefusesSessionWhosePidOnlyAnswersEPERM(t *testing.T) {
	if err := syscall.Kill(1, 0); err != syscall.EPERM {
		t.Skipf("kill(1, 0) = %v, want EPERM; run as a non-root user", err)
	}
	st, m := newStore(t)
	writeSessionFile(t, st, "1.json", uuidA, 1)
	m, _ = press(t, m, "a")
	if m.err == nil || !strings.Contains(m.View(), "is running") {
		t.Fatalf("session of another user's process not refused:\n%s", m.View())
	}
}

func TestArchiveIgnoresLivePidOfAnotherSessionAndDeadPidOfThisOne(t *testing.T) {
	st, m := newStore(t)
	writeSessionFile(t, st, "1.json", uuidB, os.Getpid())
	writeSessionFile(t, st, "2.json", uuidA, deadPID(t))
	m, _ = press(t, m, "a")
	if m.err != nil {
		t.Fatal(m.err)
	}
	mustExist(t, st.archived(uuidA+".jsonl"))
}

func TestEnterOnArchivedSessionUnarchivesThenResumes(t *testing.T) {
	fakeClaude(t)
	st, m := newStore(t)
	cwd := t.TempDir()
	m.sessions[0].Cwd = cwd
	m.refresh()
	m, _ = press(t, m, "a")
	m, _ = press(t, m, "tab")
	m, cmd := press(t, m, "enter")
	if !quits(cmd) || m.resume == nil || m.resume.uuid != uuidA {
		t.Fatalf("did not resume: %v", m.err)
	}
	mustExist(t, st.active(uuidA+".jsonl"))
	mustNotExist(t, st.archived(uuidA+".jsonl"))
}

func TestEnterOnArchivedSessionWhoseUnarchiveIsRefusedDoesNotResume(t *testing.T) {
	fakeClaude(t)
	st, m := newStore(t)
	m.sessions[0].Cwd = t.TempDir()
	m.refresh()
	m, _ = press(t, m, "a")
	m, _ = press(t, m, "tab")
	write(t, st.active(uuidA+".jsonl"), "someone else's", time.Now())
	m, cmd := press(t, m, "enter")
	if cmd != nil || m.resume != nil {
		t.Fatal("enter resumed despite the refused unarchive")
	}
	if !strings.Contains(m.View(), "already exists") {
		t.Fatalf("refusal not shown:\n%s", m.View())
	}
}

func TestTabTogglesBetweenActiveAndAllViewMarkingArchivedRows(t *testing.T) {
	m := sized(
		Session{UUID: uuidA, Title: "active one"},
		Session{UUID: uuidB, Title: "stored one", Archived: true},
	)
	if v := m.View(); !strings.Contains(v, "active one") || strings.Contains(v, "stored one") {
		t.Fatalf("active view:\n%s", v)
	}
	m, _ = press(t, m, "tab")
	v := m.View()
	if !strings.Contains(v, "[archived] stored one") || strings.Contains(v, "[archived] active one") {
		t.Fatalf("all view:\n%s", v)
	}
	m, _ = press(t, m, "tab")
	if strings.Contains(m.View(), "stored one") {
		t.Fatalf("second tab did not return to the active view:\n%s", m.View())
	}
}

func TestDataDirIsXDGDataHomeElseLocalShare(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	t.Setenv("XDG_DATA_HOME", "/xdg")
	if got, _ := dataDir(); got != filepath.Join("/xdg", "ccarchive") {
		t.Errorf("set: dataDir = %q", got)
	}
	want := filepath.Join("/home/someone", ".local", "share", "ccarchive")
	t.Setenv("XDG_DATA_HOME", "")
	if got, _ := dataDir(); got != want {
		t.Errorf("empty: dataDir = %q", got)
	}
	os.Unsetenv("XDG_DATA_HOME")
	if got, _ := dataDir(); got != want {
		t.Errorf("unset: dataDir = %q", got)
	}
}

func TestListSessionsIncludesArchivedMarked(t *testing.T) {
	st := store{cfg: t.TempDir(), data: t.TempDir()}
	write(t, st.active(uuidA+".jsonl"), "", time.Now().Add(-time.Hour))
	write(t, st.archived(uuidB+".jsonl"), "", time.Now())
	got, err := listSessions(st.cfg, st.data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].UUID != uuidB || !got[0].Archived || got[1].Archived {
		t.Fatalf("got %+v", got)
	}
}
