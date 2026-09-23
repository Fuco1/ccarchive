package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var sizeMsg = tea.WindowSizeMsg{Width: 200, Height: 40}

func sized(sessions ...Session) model {
	m, _ := newModel("", "", sessions).Update(sizeMsg)
	return m.(model)
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
	for _, k := range []string{"↑/k", "↓/j", "←/h/pgup", "→/l/pgdn", "g/home", "G/end", "enter", "q", "ctrl+c", "?"} {
		if !strings.Contains(view, k) {
			t.Errorf("help view lacks %q:\n%s", k, view)
		}
	}
	for _, re := range []string{`\ba\s+archive/unarchive`, `\btab\s+active/all`} {
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
	m, _ := press(t, sized(make([]Session, 50)...), "l")
	for _, k := range []string{"b", "u", "f", "d"} {
		m, _ = press(t, m, k)
		if got := m.list.Paginator.Page; got != 1 {
			t.Fatalf("after %s page = %d, want 1", k, got)
		}
	}
}
