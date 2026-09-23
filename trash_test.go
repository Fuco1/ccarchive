package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	uuidD = "44444444-4444-4444-8444-444444444444"
	uuidE = "55555555-5555-4555-8555-555555555555"
)

// listSessions fails without <config>/projects.
func emptyStore(t *testing.T) store {
	t.Helper()
	st := store{cfg: t.TempDir(), data: t.TempDir()}
	if err := os.Mkdir(filepath.Join(st.cfg, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	return st
}

func (st store) trashed(name string) string { return filepath.Join(st.data, "trash", project, name) }

func putInTrash(t *testing.T, st store, uuid, title, sc string) {
	t.Helper()
	write(t, st.trashed(uuid+".jsonl"),
		`{"type":"custom-title","customTitle":"`+title+`"}`+"\n"+`{"type":"user","cwd":"cwd-`+title+`"}`+"\n",
		time.Now().Add(-100*24*time.Hour))
	write(t, st.trashed(filepath.Join(uuid, "f.txt")), "sibling", time.Now())
	if sc != "" {
		write(t, st.trashed(uuid+".trashed"), sc, time.Now())
	}
}

func sidecarJSON(t *testing.T, at time.Time, origin string) string {
	t.Helper()
	b, err := json.Marshal(sidecar{at, origin})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readSidecarFile(t *testing.T, path string) sidecar {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sc sidecar
	if err := json.Unmarshal(b, &sc); err != nil {
		t.Fatal(err)
	}
	return sc
}

func TestTrashMovesSessionAndWritesSidecarWithTimeAndOrigin(t *testing.T) {
	for _, tc := range []struct {
		origin string
		keys   []string
		src    func(store, string) string
	}{
		{originActive, nil, store.active},
		{originArchive, []string{"a", "tab"}, store.archived},
	} {
		t.Run(tc.origin, func(t *testing.T) {
			st, m := newStore(t)
			for _, k := range tc.keys {
				m, _ = press(t, m, k)
			}
			before := time.Now()
			m, _ = press(t, m, "d")
			if m.err != nil {
				t.Fatal(m.err)
			}
			mustExist(t, st.trashed(uuidA+".jsonl"), st.trashed(filepath.Join(uuidA, "sub", "f.txt")))
			mustNotExist(t, tc.src(st, uuidA+".jsonl"), tc.src(st, uuidA))
			sc := readSidecarFile(t, st.trashed(uuidA+".trashed"))
			if sc.Origin != tc.origin {
				t.Errorf("origin = %q, want %q", sc.Origin, tc.origin)
			}
			if sc.DeletedAt.Before(before) || sc.DeletedAt.After(time.Now()) {
				t.Errorf("deletedAt = %v, not the time of the d", sc.DeletedAt)
			}
		})
	}
}

func TestTrashedSessionIsInNeitherActiveNorAllView(t *testing.T) {
	st, m := newStore(t)
	m, _ = press(t, m, "d")
	if m.err != nil {
		t.Fatal(m.err)
	}
	for _, m := range []model{m, st.model(t)} {
		if strings.Contains(m.View(), "hello") {
			t.Errorf("active view lists it:\n%s", m.View())
		}
		m, _ = press(t, m, "tab")
		if strings.Contains(m.View(), "hello") {
			t.Errorf("active and archived view lists it:\n%s", m.View())
		}
	}
}

func TestTrashRefusesLiveSession(t *testing.T) {
	st, m := newStore(t)
	writeSessionFile(t, st, "1.json", uuidA, os.Getpid())
	m, _ = press(t, m, "d")
	if m.err == nil || !strings.Contains(m.View(), "is running") {
		t.Fatalf("live session not refused:\n%s", m.View())
	}
	mustExist(t, st.active(uuidA+".jsonl"))
	mustNotExist(t, st.trashed(uuidA+".jsonl"), st.trashed(uuidA+".trashed"))
}

func TestTrashRefusesExistingDestination(t *testing.T) {
	st, m := newStore(t)
	write(t, st.trashed(uuidA+".jsonl"), "someone else's", time.Now())
	m, _ = press(t, m, "d")
	if m.err == nil || !strings.Contains(m.View(), "already exists") {
		t.Fatalf("not refused:\n%s", m.View())
	}
	mustExist(t, st.active(uuidA+".jsonl"), st.active(filepath.Join(uuidA, "sub", "f.txt")))
	mustNotExist(t, st.trashed(uuidA+".trashed"))
}

func TestTrashRefusesExistingSiblingDirWhenSessionHasNone(t *testing.T) {
	st, m := newStore(t)
	if err := os.RemoveAll(st.active(uuidA)); err != nil {
		t.Fatal(err)
	}
	write(t, st.trashed(filepath.Join(uuidA, "theirs")), "someone else's", time.Now())
	m, _ = press(t, m, "d")
	if m.err == nil || !strings.Contains(m.View(), "already exists") {
		t.Fatalf("not refused:\n%s", m.View())
	}
	mustExist(t, st.active(uuidA+".jsonl"))
	mustNotExist(t, st.trashed(uuidA+".jsonl"), st.trashed(uuidA+".trashed"))
}

func TestTrashSiblingDirMoveFailureMovesJsonlBack(t *testing.T) {
	st, m := newStore(t)
	failRename(t, func(src string) bool { return filepath.Base(src) == uuidA }, syscall.EACCES)
	m, _ = press(t, m, "d")
	if m.err == nil || !strings.Contains(m.View(), "permission denied") {
		t.Fatalf("failure not shown:\n%s", m.View())
	}
	mustExist(t, st.active(uuidA+".jsonl"), st.active(filepath.Join(uuidA, "sub", "f.txt")))
	mustNotExist(t, st.trashed(uuidA+".jsonl"), st.trashed(uuidA), st.trashed(uuidA+".trashed"))
	if m.sessions[0].Trashed {
		t.Error("model records the session as trashed")
	}
}

func TestTrashViewListsTitleCwdAndDeletionTimeOrUnknown(t *testing.T) {
	st := emptyStore(t)
	at := time.Date(2026, 9, 1, 12, 30, 0, 0, time.Local)
	putInTrash(t, st, uuidA, "known", sidecarJSON(t, at, originActive))
	putInTrash(t, st, uuidB, "garbled", "{not json")
	putInTrash(t, st, uuidC, "missing", "")
	m, _ := press(t, st.model(t), "T")
	v := m.View()
	for _, want := range []string{
		"known", "cwd-known  deleted 2026-09-01 12:30",
		"garbled", "cwd-garbled  deleted unknown",
		"missing", "cwd-missing  deleted unknown",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("trash view lacks %q:\n%s", want, v)
		}
	}
}

func TestTrashViewListsOnlyTrashedSessions(t *testing.T) {
	st, _ := newStore(t)
	putInTrash(t, st, uuidB, "binned", sidecarJSON(t, time.Now(), originActive))
	m, _ := press(t, st.model(t), "T")
	if v := m.View(); !strings.Contains(v, "binned") || strings.Contains(v, "hello") {
		t.Fatalf("trash view:\n%s", v)
	}
}

func TestEscInTrashViewClearsSearchThenReturnsToPreviousView(t *testing.T) {
	m := sized(
		Session{UUID: uuidA, Title: "stored one", Archived: true},
		Session{UUID: uuidB, Title: "binned one", Trashed: true},
		Session{UUID: uuidC, Title: "binned two", Trashed: true},
	)
	m, _ = press(t, m, "tab")
	m, _ = press(t, m, "T")
	m, _ = press(t, m, "/")
	m, _ = press(t, m, "two")
	m, _ = press(t, m, "enter")
	if strings.Contains(m.View(), "binned one") {
		t.Fatalf("filter not applied:\n%s", m.View())
	}
	m, _ = press(t, m, "esc")
	if v := m.View(); !strings.Contains(v, "binned one") || strings.Contains(v, "stored one") {
		t.Fatalf("first esc did not just clear the search:\n%s", v)
	}
	m, _ = press(t, m, "esc")
	if v := m.View(); !strings.Contains(v, "[archived] stored one") || strings.Contains(v, "binned") {
		t.Fatalf("second esc did not return to the active and archived view:\n%s", v)
	}
}

func TestRestoreMovesSessionToItsOriginCreatingProjectDirAndRemovesSidecar(t *testing.T) {
	for _, tc := range []struct {
		name string
		sc   func(t *testing.T) string
		dst  func(store, string) string
	}{
		{"active", func(t *testing.T) string { return sidecarJSON(t, time.Now(), originActive) }, store.active},
		{"archive", func(t *testing.T) string { return sidecarJSON(t, time.Now(), originArchive) }, store.archived},
		{"unreadable sidecar", func(*testing.T) string { return `{"origin":"archive"` }, store.active},
		{"missing sidecar", func(*testing.T) string { return "" }, store.active},
		{"unknown origin", func(t *testing.T) string { return sidecarJSON(t, time.Now(), "elsewhere") }, store.active},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := emptyStore(t)
			putInTrash(t, st, uuidA, "binned", tc.sc(t))
			if exists(filepath.Dir(tc.dst(st, uuidA+".jsonl"))) {
				t.Fatal("destination project dir exists before the restore")
			}
			m, _ := press(t, st.model(t), "T")
			m, _ = press(t, m, "u")
			if m.err != nil {
				t.Fatal(m.err)
			}
			mustExist(t, tc.dst(st, uuidA+".jsonl"), tc.dst(st, filepath.Join(uuidA, "f.txt")))
			mustNotExist(t, st.trashed(uuidA+".jsonl"), st.trashed(uuidA), st.trashed(uuidA+".trashed"))
			if strings.Contains(m.View(), "binned") {
				t.Errorf("trash view still lists it:\n%s", m.View())
			}
		})
	}
}

func TestRestoreRefusesExistingSiblingDirWhenSessionHasNone(t *testing.T) {
	st := emptyStore(t)
	putInTrash(t, st, uuidA, "binned", sidecarJSON(t, time.Now(), originActive))
	if err := os.RemoveAll(st.trashed(uuidA)); err != nil {
		t.Fatal(err)
	}
	write(t, st.active(filepath.Join(uuidA, "theirs")), "someone else's", time.Now())
	m, _ := press(t, st.model(t), "T")
	m, _ = press(t, m, "u")
	if m.err == nil || !strings.Contains(m.View(), "already exists") {
		t.Fatalf("not refused:\n%s", m.View())
	}
	mustExist(t, st.trashed(uuidA+".jsonl"), st.trashed(uuidA+".trashed"))
	mustNotExist(t, st.active(uuidA+".jsonl"))
}

func TestRestoreRefusesExistingDestination(t *testing.T) {
	st := emptyStore(t)
	putInTrash(t, st, uuidA, "binned", sidecarJSON(t, time.Now(), originActive))
	write(t, st.active(uuidA+".jsonl"), "someone else's", time.Now())
	m, _ := press(t, st.model(t), "T")
	m, _ = press(t, m, "u")
	if m.err == nil || !strings.Contains(m.View(), "already exists") {
		t.Fatalf("not refused:\n%s", m.View())
	}
	mustExist(t, st.trashed(uuidA+".jsonl"), st.trashed(filepath.Join(uuidA, "f.txt")), st.trashed(uuidA+".trashed"))
	if b, _ := os.ReadFile(st.active(uuidA + ".jsonl")); string(b) != "someone else's" {
		t.Errorf("destination overwritten: %q", b)
	}
}

func purgeable(t *testing.T) (store, []string, string) {
	t.Helper()
	st := emptyStore(t)
	putInTrash(t, st, uuidA, "binned", sidecarJSON(t, time.Now(), originActive))
	fh := filepath.Join(st.cfg, "file-history", uuidA, "x")
	env := filepath.Join(st.cfg, "session-env", uuidA, "x")
	other := filepath.Join(st.cfg, "file-history", uuidB, "x")
	for _, p := range []string{fh, env, other} {
		write(t, p, "", time.Now())
	}
	return st, []string{
		st.trashed(uuidA + ".jsonl"), st.trashed(uuidA), st.trashed(uuidA + ".trashed"),
		filepath.Dir(fh), filepath.Dir(env),
	}, other
}

func TestPurgeOnYDeletesSessionSidecarAndClaudeDirs(t *testing.T) {
	st, owned, other := purgeable(t)
	m, _ := press(t, st.model(t), "T")
	m, _ = press(t, m, "X")
	if !strings.Contains(m.View(), "y/n") {
		t.Fatalf("no confirmation asked:\n%s", m.View())
	}
	mustExist(t, owned...)
	m, _ = press(t, m, "y")
	if m.err != nil {
		t.Fatal(m.err)
	}
	mustNotExist(t, owned...)
	mustExist(t, other)
	if strings.Contains(m.View(), "binned") {
		t.Errorf("trash view still lists it:\n%s", m.View())
	}
}

func TestPurgeOnAnyOtherKeyDeletesNothing(t *testing.T) {
	for _, k := range []string{"n", "Y", "X", "esc", "enter"} {
		t.Run(k, func(t *testing.T) {
			st, owned, _ := purgeable(t)
			m, _ := press(t, st.model(t), "T")
			m, _ = press(t, m, "X")
			m, _ = press(t, m, k)
			mustExist(t, owned...)
			if strings.Contains(m.View(), "y/n") {
				t.Errorf("still asking after %s:\n%s", k, m.View())
			}
			m, _ = press(t, m, "y")
			mustExist(t, owned...)
		})
	}
}

func TestPurgeRefusesNonCanonicalUUIDBeforeTouchingAnything(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", uuidA + "x", "x" + uuidA} {
		t.Run(bad, func(t *testing.T) {
			st, owned, other := purgeable(t)
			// Each is a real directory, so a purge that built its path deletes it.
			victim := filepath.Join(st.cfg, "file-history", bad)
			if err := os.MkdirAll(victim, 0o755); err != nil {
				t.Fatal(err)
			}
			err := purge(Session{UUID: bad, Project: project, Trashed: true}, st.cfg, st.data)
			if err == nil || !strings.Contains(err.Error(), "not a canonical UUID") {
				t.Fatalf("purge(%q) = %v", bad, err)
			}
			mustExist(t, victim, other)
			mustExist(t, owned...)
		})
	}
}

func TestPurgeRefusesSessionNotInTrash(t *testing.T) {
	st, owned, _ := purgeable(t)
	if err := purge(Session{UUID: uuidA, Project: project}, st.cfg, st.data); err == nil {
		t.Fatal("purged a session not marked as trashed")
	}
	mustExist(t, owned...)
}

func TestStartupPurgesOnlyEntriesPastThirtyDaysWithReadableSidecar(t *testing.T) {
	st := emptyStore(t)
	now := time.Now()
	putInTrash(t, st, uuidA, "expired", sidecarJSON(t, now.Add(-trashRetention-time.Minute), originActive))
	putInTrash(t, st, uuidB, "recent", sidecarJSON(t, now.Add(-trashRetention+time.Minute), originActive))
	putInTrash(t, st, uuidC, "garbled", "{not json")
	putInTrash(t, st, uuidD, "missing", "")
	putInTrash(t, st, uuidE, "odd origin", sidecarJSON(t, now.Add(-2*trashRetention), "elsewhere"))
	for _, u := range []string{uuidA, uuidB, uuidC, uuidD, uuidE} {
		write(t, filepath.Join(st.cfg, "file-history", u, "x"), "", now)
	}
	sessions, err := listSessions(st.cfg, st.data)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := purgeExpired(sessions, st.cfg, st.data, now)
	if err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, st.trashed(uuidA+".jsonl"), st.trashed(uuidA), st.trashed(uuidA+".trashed"),
		filepath.Join(st.cfg, "file-history", uuidA))
	for _, u := range []string{uuidB, uuidC, uuidD, uuidE} {
		mustExist(t, st.trashed(u+".jsonl"), st.trashed(u), filepath.Join(st.cfg, "file-history", u))
	}
	var got []string
	for _, s := range kept {
		got = append(got, s.UUID)
	}
	if len(got) != 4 || strings.Contains(strings.Join(got, " "), uuidA) {
		t.Errorf("kept %v", got)
	}
}

func TestTrashRefusesExistingSidecarAndMovesSessionBack(t *testing.T) {
	st, m := newStore(t)
	write(t, st.trashed(uuidA+".trashed"), "someone else's", time.Now())
	m, _ = press(t, m, "d")
	if m.err == nil || !strings.Contains(m.View(), "exists") {
		t.Fatalf("not refused:\n%s", m.View())
	}
	mustExist(t, st.active(uuidA+".jsonl"), st.active(filepath.Join(uuidA, "sub", "f.txt")))
	mustNotExist(t, st.trashed(uuidA+".jsonl"), st.trashed(uuidA))
	if b, _ := os.ReadFile(st.trashed(uuidA + ".trashed")); string(b) != "someone else's" {
		t.Errorf("existing sidecar replaced: %q", b)
	}
}

func TestSessionKeysDoNothingInTrashView(t *testing.T) {
	fakeClaude(t)
	st := emptyStore(t)
	putInTrash(t, st, uuidA, "binned", sidecarJSON(t, time.Now(), originArchive))
	m, _ := press(t, st.model(t), "T")
	m.sessions[0].Cwd = t.TempDir()
	for _, k := range []string{"enter", "a", "d", "tab", "T"} {
		var cmd tea.Cmd
		m, cmd = press(t, m, k)
		if m.err != nil || cmd != nil || m.resume != nil || !m.trash || m.showAll {
			t.Fatalf("%s acted in the trash view: err=%v resume=%v trash=%v showAll=%v", k, m.err, m.resume, m.trash, m.showAll)
		}
	}
	mustExist(t, st.trashed(uuidA+".jsonl"), st.trashed(uuidA+".trashed"))
	mustNotExist(t, st.active(uuidA+".jsonl"), st.archived(uuidA+".jsonl"))
}
