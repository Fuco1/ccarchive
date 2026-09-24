package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

var sizeMsg = tea.WindowSizeMsg{Width: 200, Height: 40}

// sized starts in the all-projects scope, so tests that are not about the
// scope need not give their sessions the working directory's cwd.
func sized(sessions ...Session) model {
	return allProjects(newModel("", "", "", sessions))
}

func allProjects(m model) model {
	m.allProjects = true
	m.refresh()
	next, _ := m.Update(sizeMsg)
	return next.(model)
}

func press(t *testing.T, m model, k string) (model, tea.Cmd) {
	t.Helper()
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+f":
		msg = tea.KeyMsg{Type: tea.KeyCtrlF}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, cmd := m.Update(msg)
	return next.(model), cmd
}

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func fakeClaude(t *testing.T) string {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return path
}

func TestEnterResumesSelectedSessionAndQuits(t *testing.T) {
	claude := fakeClaude(t)
	cwd := t.TempDir()
	m := sized(Session{UUID: uuidA, Cwd: t.TempDir()}, Session{UUID: uuidB, Cwd: cwd})
	m, _ = press(t, m, "j")
	m, cmd := press(t, m, "enter")
	if !quits(cmd) {
		t.Fatal("enter did not quit the program")
	}
	want := resumeTarget{claude: claude, cwd: cwd, uuid: uuidB}
	if m.resume == nil || *m.resume != want {
		t.Fatalf("resume = %+v, want %+v", m.resume, want)
	}
}

func TestEnterWithMissingCwdShowsErrorAndStays(t *testing.T) {
	fakeClaude(t)
	m := sized(Session{UUID: uuidA, Cwd: filepath.Join(t.TempDir(), "gone")})
	m, cmd := press(t, m, "enter")
	if cmd != nil || m.resume != nil {
		t.Fatal("enter proceeded despite a missing cwd")
	}
	if !strings.Contains(m.View(), "gone") {
		t.Fatalf("error not shown:\n%s", m.View())
	}
}

func TestEnterWithoutClaudeOnPathShowsErrorAndStays(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m := sized(Session{UUID: uuidA, Cwd: t.TempDir()})
	m, cmd := press(t, m, "enter")
	if cmd != nil || m.resume != nil {
		t.Fatal("enter proceeded without claude on PATH")
	}
	if !strings.Contains(m.View(), `"claude"`) {
		t.Fatalf("error not shown:\n%s", m.View())
	}
}

func TestUpDownAndKJMoveSelection(t *testing.T) {
	m := sized(Session{UUID: uuidA}, Session{UUID: uuidB}, Session{UUID: uuidC})
	for _, step := range []struct {
		key  string
		want int
	}{{"j", 1}, {"down", 2}, {"k", 1}, {"up", 0}} {
		m, _ = press(t, m, step.key)
		if got := m.list.Index(); got != step.want {
			t.Fatalf("after %s index = %d, want %d", step.key, got, step.want)
		}
	}
}

func TestQQuits(t *testing.T) {
	if _, cmd := press(t, sized(Session{UUID: uuidA}), "q"); !quits(cmd) {
		t.Fatal("q did not quit")
	}
}

func TestHelpViewListsEveryBoundKey(t *testing.T) {
	// Page keys are bound only while there is more than one page.
	many := make([]Session, 50)
	m, _ := press(t, sized(many...), "?")
	view := m.View()
	// Walking the fields catches a binding added to keyMap without help text.
	km := reflect.ValueOf(m.list.KeyMap)
	for i := 0; i < km.NumField(); i++ {
		b := km.Field(i).Interface().(key.Binding)
		if b.Enabled() && (b.Help().Key == "" || !strings.Contains(view, b.Help().Key)) {
			t.Errorf("help view lacks %s key %q:\n%s", km.Type().Field(i).Name, b.Help().Key, view)
		}
	}
	for _, re := range []string{`\ba\s+archive/unarchive`, `\btab\s+active/all`, `(^|\s)/\s+search`, `\bctrl\+f\s+in search, toggle full-text`,
		`\bp\s+this directory/all projects`, `\bd\s+move to trash`, `\bT\s+trash view`, `\bu\s+in trash, restore`, `\bX\s+in trash, purge now`} {
		if !regexp.MustCompile(re).MatchString(view) {
			t.Errorf("help view lacks %s:\n%s", re, view)
		}
	}
}

func TestRowShowsTitleCwdAndAge(t *testing.T) {
	m := sized(Session{UUID: uuidA, Title: "my title", Cwd: "/work/dir", ModTime: time.Now().Add(-3 * time.Hour)})
	view := m.View()
	for _, want := range []string{"my title", "/work/dir", "3h ago"} {
		if !strings.Contains(view, want) {
			t.Errorf("row lacks %q:\n%s", want, view)
		}
	}
}

// The list's defaults also page on b, u, f and d without listing them in help.
func TestUnlistedDefaultPageKeysAreUnbound(t *testing.T) {
	// d trashes the selected session, so the stores are real directories.
	m, _ := press(t, allProjects(newModel(t.TempDir(), t.TempDir(), "", make([]Session, 50))), "l")
	for _, k := range []string{"b", "u", "f", "d"} {
		m, _ = press(t, m, k)
		if got := m.list.Paginator.Page; got != 1 {
			t.Fatalf("after %s page = %d, want 1", k, got)
		}
	}
}

func TestDefaultScopeListsOnlySessionsWhoseCleanedCwdEqualsTheWorkingDirectory(t *testing.T) {
	m := newModel("", "", "/work/dir/", []Session{
		{UUID: uuidA, Cwd: "/work/dir"},
		{UUID: uuidB, Cwd: "/work/./dir/"},
		{UUID: uuidC, Cwd: "/work/dir/sub"},
		{UUID: "no-cwd"},
		{UUID: "parent", Cwd: "/work"},
	})
	wantRows(t, m, uuidA, uuidB)
	if !strings.Contains(m.list.Title, "/work/dir") {
		t.Fatalf("title %q does not name the directory", m.list.Title)
	}
	m, _ = press(t, m, "p")
	wantRows(t, m, uuidA, uuidB, uuidC, "no-cwd", "parent")
	if !strings.Contains(m.list.Title, "all projects") {
		t.Fatalf("title %q does not say all projects", m.list.Title)
	}
	m, _ = press(t, m, "p")
	wantRows(t, m, uuidA, uuidB)
}

func TestScopeAppliesToAllAndTrashViews(t *testing.T) {
	m := newModel("", "", "/here", []Session{
		{UUID: uuidA, Cwd: "/here"},
		{UUID: uuidB, Cwd: "/here", Archived: true},
		{UUID: uuidC, Cwd: "/here", Trashed: true},
		{UUID: "far", Cwd: "/there"},
		{UUID: "far-archived", Cwd: "/there", Archived: true},
		{UUID: "far-trashed", Cwd: "/there", Trashed: true},
	})
	m, _ = press(t, m, "tab")
	wantRows(t, m, uuidA, uuidB)
	m, _ = press(t, m, "T")
	wantRows(t, m, uuidC)
	if !strings.Contains(m.list.Title, "Trash") || !strings.Contains(m.list.Title, "/here") {
		t.Fatalf("trash title %q names neither view and scope", m.list.Title)
	}
	m, _ = press(t, m, "p")
	wantRows(t, m, uuidC, "far-trashed")
	m, _ = press(t, m, "esc")
	wantRows(t, m, uuidA, uuidB, "far", "far-archived")
}

func TestFullTextSearchesOnlyTranscriptsInScope(t *testing.T) {
	s := transcripts(t, needle+"\n", needle+"\n")
	s[1].Cwd = "/elsewhere"
	m := withScan(newModel("", "", s[0].Cwd, s))
	m, _ = press(t, m, "/")
	m, _ = press(t, m, "ctrl+f")
	m, _ = press(t, m, needle)
	_, cmd := press(t, m, "enter")
	if got := cmd().(fullTextMsg).paths; len(got) != 1 || got[0] != s[0].Path {
		t.Fatalf("searched %v, want only %s", got, s[0].Path)
	}
}
