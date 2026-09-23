package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	uuidA = "11111111-1111-4111-8111-111111111111"
	uuidB = "22222222-2222-4222-8222-222222222222"
	uuidC = "33333333-3333-4333-8333-333333333333"
)

func write(t *testing.T, path, content string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestListSessionsListsOnlyCanonicalUUIDFilesNewestFirst(t *testing.T) {
	cfg := t.TempDir()
	p1 := filepath.Join(cfg, "projects", "-home-a")
	p2 := filepath.Join(cfg, "projects", "-home-b")
	now := time.Now()
	write(t, filepath.Join(p1, uuidA+".jsonl"), "", now.Add(-3*time.Hour))
	write(t, filepath.Join(p2, uuidB+".jsonl"), "", now.Add(-1*time.Hour))
	write(t, filepath.Join(p1, uuidC+".jsonl"), "", now.Add(-2*time.Hour))
	for _, name := range []string{
		uuidA + ".orphaned-1788640852223-b64662c9.jsonl",
		"AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA.jsonl",
		uuidA + ".json",
		"notes.jsonl",
	} {
		write(t, filepath.Join(p1, name), "", now)
	}
	write(t, filepath.Join(p1, uuidA, "x.jsonl"), "", now)
	write(t, filepath.Join(cfg, "projects", uuidC+".jsonl"), "", now)
	if err := os.Mkdir(filepath.Join(p2, uuidC+".jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := listSessions(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var uuids []string
	for _, s := range got {
		uuids = append(uuids, s.UUID)
	}
	want := []string{uuidB, uuidC, uuidA}
	if strings.Join(uuids, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", uuids, want)
	}
	if got[0].Project != "-home-b" {
		t.Errorf("project = %q", got[0].Project)
	}
}

func parseTranscript(t *testing.T, lines ...string) Session {
	t.Helper()
	path := filepath.Join(t.TempDir(), uuidA+".jsonl")
	write(t, path, strings.Join(lines, "\n")+"\n", time.Now())
	s := Session{UUID: uuidA, Path: path}
	if err := s.parse(); err != nil {
		t.Fatal(err)
	}
	return s
}

const (
	metaUser   = `{"type":"user","isMeta":true,"message":{"content":"<caveat>injected</caveat>"}}`
	toolUser   = `{"type":"user","message":{"content":[{"type":"tool_result","content":"out"}]}}`
	arrayUser  = `{"type":"user","message":{"content":[{"type":"image"},{"type":"text","text":"fix the build\nplease"}]}}`
	stringUser = `{"type":"user","message":{"content":"second prompt"}}`
	ai1        = `{"type":"ai-title","aiTitle":"ai one"}`
	ai2        = `{"type":"ai-title","aiTitle":"ai two"}`
	custom1    = `{"type":"custom-title","customTitle":"custom one"}`
	custom2    = `{"type":"custom-title","customTitle":"custom two"}`
)

func TestTitleIsLastCustomTitle(t *testing.T) {
	s := parseTranscript(t, custom1, ai1, arrayUser, custom2, ai2)
	if s.Title != "custom two" {
		t.Fatalf("title = %q", s.Title)
	}
}

func TestTitleFallsBackToLastAiTitle(t *testing.T) {
	s := parseTranscript(t, ai1, arrayUser, ai2)
	if s.Title != "ai two" {
		t.Fatalf("title = %q", s.Title)
	}
}

func TestTitleFallsBackToFirstTypedUserPromptFirstLine(t *testing.T) {
	s := parseTranscript(t, metaUser, toolUser, arrayUser, stringUser)
	if s.Title != "fix the build" {
		t.Fatalf("title = %q", s.Title)
	}
	s = parseTranscript(t, metaUser, stringUser, arrayUser)
	if s.Title != "second prompt" {
		t.Fatalf("string content: title = %q", s.Title)
	}
}

func TestTitleFallsBackToUUID(t *testing.T) {
	s := parseTranscript(t, metaUser, toolUser, `{"type":"assistant"}`)
	if s.Title != uuidA {
		t.Fatalf("title = %q", s.Title)
	}
}

func TestCwdIsFirstRecordCarryingOne(t *testing.T) {
	s := parseTranscript(t, `{"type":"summary"}`, `{"type":"user","cwd":"/first"}`, `{"type":"user","cwd":"/second"}`)
	if s.Cwd != "/first" {
		t.Fatalf("cwd = %q", s.Cwd)
	}
}

func TestCwdIsFirstRecordCarryingOneEvenWhenEmpty(t *testing.T) {
	s := parseTranscript(t, `{"type":"user","cwd":""}`, `{"type":"user","cwd":"/later"}`)
	if s.Cwd != "" {
		t.Fatalf("cwd = %q", s.Cwd)
	}
}

func TestParseReadsLinesPastOneMiBAndSkipsOnlyMalformedLines(t *testing.T) {
	big := `{"type":"user","cwd":"/big","message":{"content":"` + strings.Repeat("x", 2<<20) + `"}}`
	s := parseTranscript(t,
		big,
		`{"type":"custom-title","customTitle":"broken"`,
		custom1,
	)
	if s.Cwd != "/big" {
		t.Errorf("cwd = %q, the long line was not parsed", s.Cwd)
	}
	if s.Title != "custom one" {
		t.Errorf("title = %q, the line after the malformed one was not parsed", s.Title)
	}
}

func TestConfigDirHonoursClaudeConfigDirEvenWhenEmpty(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	for _, v := range []string{"/custom", ""} {
		t.Setenv("CLAUDE_CONFIG_DIR", v)
		if got, _ := configDir(); got != v {
			t.Errorf("CLAUDE_CONFIG_DIR=%q: configDir = %q", v, got)
		}
	}
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	if got, _ := configDir(); got != filepath.Join("/home/someone", ".claude") {
		t.Errorf("unset: configDir = %q", got)
	}
}
