package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Session struct {
	UUID    string
	Project string
	Path    string
	ModTime time.Time
	Title   string
	Cwd     string
	Prompt  string
	// For a trashed session Archived is the origin its sidecar records, which
	// is where a restore puts it back.
	Archived bool
	Trashed  bool
	// Zero when the sidecar is unreadable.
	TrashedAt time.Time
}

// A project directory also holds files like <uuid>.orphaned-<n>-<hex>.jsonl;
// later milestones move whatever is listed, so only canonical names count.
const uuidPattern = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`

var (
	sessionName   = regexp.MustCompile(`^` + uuidPattern + `\.jsonl$`)
	canonicalUUID = regexp.MustCompile(`^` + uuidPattern + `$`)
)

func configDir() (string, error) {
	// The spec's rule is "when set", and set-but-empty is set.
	if d, ok := os.LookupEnv("CLAUDE_CONFIG_DIR"); ok {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// dataDir follows the XDG rule that an empty XDG_DATA_HOME counts as unset,
// which configDir's reading of CLAUDE_CONFIG_DIR does not.
func dataDir() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "ccarchive"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "ccarchive"), nil
}

func activeRoot(config string) string { return filepath.Join(config, "projects") }
func archiveRoot(data string) string  { return filepath.Join(data, "archive") }
func trashRoot(data string) string    { return filepath.Join(data, "trash") }

// The archive and the trash do not exist until a session is first moved there.
func listSessions(config, data string) ([]Session, error) {
	out, err := scanSessions(activeRoot(config), false)
	if err != nil {
		return nil, err
	}
	archived, err := scanSessions(archiveRoot(data), true)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	out = append(out, archived...)
	trashed, err := scanSessions(trashRoot(data), false)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for i := range trashed {
		t := &trashed[i]
		t.Trashed = true
		t.TrashedAt, t.Archived = readSidecar(sidecarPath(filepath.Dir(t.Path), t.UUID))
	}
	out = append(out, trashed...)
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

func scanSessions(projects string, archived bool) ([]Session, error) {
	dirs, err := os.ReadDir(projects)
	if err != nil {
		return nil, err
	}
	var out []Session
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(projects, d.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if !f.Type().IsRegular() || !sessionName.MatchString(f.Name()) {
				continue
			}
			info, err := f.Info()
			if err != nil {
				return nil, err
			}
			s := Session{
				UUID:     strings.TrimSuffix(f.Name(), ".jsonl"),
				Project:  d.Name(),
				Path:     filepath.Join(projects, d.Name(), f.Name()),
				ModTime:  info.ModTime(),
				Archived: archived,
			}
			if err := s.parse(); err != nil {
				return nil, err
			}
			out = append(out, s)
		}
	}
	return out, nil
}

type record struct {
	Type        string  `json:"type"`
	CustomTitle string  `json:"customTitle"`
	AiTitle     string  `json:"aiTitle"`
	Cwd         *string `json:"cwd"`
	IsMeta      bool    `json:"isMeta"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// Lines run to hundreds of KiB because attachments are inlined, so this reads
// with ReadBytes rather than a bufio.Scanner, whose token limit would drop them.
func (s *Session) parse() error {
	f, err := os.Open(s.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var custom, ai string
	cwdSeen := false
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var rec record
			if json.Unmarshal(line, &rec) == nil {
				if !cwdSeen && rec.Cwd != nil {
					s.Cwd, cwdSeen = *rec.Cwd, true
				}
				switch rec.Type {
				case "custom-title":
					if rec.CustomTitle != "" {
						custom = rec.CustomTitle
					}
				case "ai-title":
					if rec.AiTitle != "" {
						ai = rec.AiTitle
					}
				case "user":
					if s.Prompt == "" && !rec.IsMeta {
						s.Prompt = strings.TrimSpace(contentText(rec.Message.Content))
					}
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	s.Title = s.UUID
	for _, t := range []string{custom, ai, firstLine(s.Prompt)} {
		if t != "" {
			s.Title = t
			break
		}
	}
	return nil
}

func contentText(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var items []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	var texts []string
	if json.Unmarshal(raw, &items) == nil {
		for _, it := range items {
			if it.Type == "text" && strings.TrimSpace(it.Text) != "" {
				texts = append(texts, it.Text)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
