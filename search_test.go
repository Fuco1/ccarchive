package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rows(m model) []string {
	var out []string
	for _, it := range m.list.Items() {
		out = append(out, it.(item).UUID)
	}
	return out
}

func wantRows(t *testing.T, m model, want ...string) {
	t.Helper()
	if got := rows(m); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestFilterMatchesTitleCwdOrPromptCaseInsensitivelyAndEscRestores(t *testing.T) {
	m := sized(
		Session{UUID: uuidA, Title: "Fix the Parser"},
		Session{UUID: uuidB, Title: "b", Cwd: "/work/ParserLand"},
		Session{UUID: uuidC, Title: "c", Prompt: "first line\nthen the PARSER"},
	)
	m, _ = press(t, m, "/")
	for _, step := range []struct {
		typed string
		want  []string
	}{
		{"parser", []string{uuidA, uuidB, uuidC}},
		{"land", []string{uuidB}},
	} {
		m, _ = press(t, m, step.typed)
		wantRows(t, m, step.want...)
	}
	m.input.SetValue("")
	m, _ = press(t, m, "then the")
	wantRows(t, m, uuidC)
	m.input.SetValue("")
	m, _ = press(t, m, "fix")
	wantRows(t, m, uuidA)
	if !strings.Contains(m.View(), "filter: fix") {
		t.Fatalf("prompt not shown:\n%s", m.View())
	}

	m, _ = press(t, m, "enter")
	m, _ = press(t, m, "j")
	wantRows(t, m, uuidA)
	m, _ = press(t, m, "esc")
	wantRows(t, m, uuidA, uuidB, uuidC)
	if strings.Contains(m.View(), "filter:") {
		t.Fatalf("prompt still shown:\n%s", m.View())
	}

	m, _ = press(t, m, "/")
	m, _ = press(t, m, "land")
	m, _ = press(t, m, "esc")
	wantRows(t, m, uuidA, uuidB, uuidC)
}

func TestCtrlFTogglesFullTextAndPromptNamesTheEngine(t *testing.T) {
	m := sized(Session{UUID: uuidA, Title: "a"})
	m.rg = "/usr/bin/rg"
	m, _ = press(t, m, "/")
	for _, want := range []string{"full-text (rg): ", "filter: "} {
		m, _ = press(t, m, "ctrl+f")
		if !strings.Contains(m.View(), want) {
			t.Fatalf("prompt lacks %q:\n%s", want, m.View())
		}
	}
	m.rg = ""
	m, _ = press(t, m, "ctrl+f")
	if !strings.Contains(m.View(), "full-text (scan): ") {
		t.Fatalf("prompt does not say scan:\n%s", m.View())
	}
	// Typing alone must not spawn a process per character.
	m, _ = press(t, m, "zzz")
	wantRows(t, m, uuidA)
}

const needle = "Quux-Only-In-Body"

// Every title, cwd and prompt differs from the bodies, so only a full-text
// search can find them.
func transcripts(t *testing.T, bodies ...string) []Session {
	dir := filepath.Join(t.TempDir(), "-proj")
	var out []Session
	for i, body := range bodies {
		u := []string{uuidA, uuidB, uuidC}[i]
		p := filepath.Join(dir, u+".jsonl")
		write(t, p, body, time.Now())
		out = append(out, Session{UUID: u, Path: p, Title: "t", Cwd: "/c", Prompt: "p"})
	}
	return out
}

func fullText(t *testing.T, m model, query string) model {
	t.Helper()
	m, _ = press(t, m, "/")
	m, _ = press(t, m, "ctrl+f")
	m, _ = press(t, m, query)
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatal("enter ran no full-text search")
	}
	next, _ := m.Update(cmd())
	return next.(model)
}

func withRg(t *testing.T, m model) model {
	rg, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("rg is not on PATH")
	}
	m.rg = rg
	return m
}

func withScan(m model) model { m.rg = ""; return m }

func bodyFixture(t *testing.T) model {
	s := transcripts(t,
		`{"type":"assistant","text":"`+strings.Repeat("x", 1<<20)+strings.ToUpper(needle)+`"}`+"\n",
		`{"type":"user"}`+"\n",
		`{"type":"user"}`+"\n",
	)
	// Planted where the search must not look: a session's own directory, and a
	// transcript outside the view.
	write(t, filepath.Join(filepath.Dir(s[1].Path), uuidB, "sub.jsonl"), needle, time.Now())
	write(t, filepath.Join(filepath.Dir(s[1].Path), "44444444-4444-4444-8444-444444444444.jsonl"), needle, time.Now())
	return sized(s...)
}

func TestFullTextScanFindsSessionByTranscriptBody(t *testing.T) {
	m := fullText(t, withScan(bodyFixture(t)), needle)
	wantRows(t, m, uuidA)
	if m.err != nil {
		t.Fatal(m.err)
	}
	m, _ = press(t, m, "esc")
	wantRows(t, m, uuidA, uuidB, uuidC)
}

func TestFullTextRgFindsSessionByTranscriptBody(t *testing.T) {
	m := fullText(t, withRg(t, bodyFixture(t)), needle)
	wantRows(t, m, uuidA)
	if m.err != nil {
		t.Fatal(m.err)
	}
}

func TestFullTextRgMatchingNothingShowsEmptyListAndNoError(t *testing.T) {
	m := withRg(t, sized(transcripts(t, "nothing here\n")...))
	if _, err := rgSearch(m.rg, needle, []string{m.sessions[0].Path}); err != nil {
		t.Fatalf("exit 1 is an error: %v", err)
	}
	m = fullText(t, m, needle)
	wantRows(t, m)
	if m.err != nil || strings.Contains(m.View(), "error") {
		t.Fatalf("error shown for no match: %v\n%s", m.err, m.View())
	}
}

func TestFullTextRgExitTwoKeepsPrintedRowsAndShowsStderr(t *testing.T) {
	s := transcripts(t, needle+"\n", needle+"\n")
	s[1].Path = filepath.Join(t.TempDir(), "vanished.jsonl")
	m := fullText(t, withRg(t, sized(s...)), needle)
	wantRows(t, m, uuidA)
	if !strings.Contains(m.View(), "vanished.jsonl") {
		t.Fatalf("rg's stderr not shown:\n%s", m.View())
	}
}

// Session B holds the text an unquoted, flag-parsed or regex query would
// match instead, so any of those readings adds B to the rows.
func TestFullTextQueryIsSearchedLiterally(t *testing.T) {
	for _, q := range []string{`$(echo b); *|'x'`, "--files", "-v", "a.c"} {
		for name, engine := range map[string]func(*testing.T, model) model{
			"rg":   withRg,
			"scan": func(_ *testing.T, m model) model { return withScan(m) },
		} {
			t.Run(name+" "+q, func(t *testing.T) {
				m := sized(transcripts(t, "has "+q+" in it\n", "b abc x files v\n")...)
				wantRows(t, fullText(t, engine(t, m), q), uuidA)
			})
		}
	}
}
