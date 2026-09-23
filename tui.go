package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func (s Session) FilterValue() string { return s.Title }
func (s Session) Description() string {
	if !s.Trashed {
		return s.Cwd + "  " + age(time.Since(s.ModTime))
	}
	at := "unknown"
	if !s.TrashedAt.IsZero() {
		at = s.TrashedAt.Local().Format("2006-01-02 15:04")
	}
	return s.Cwd + "  deleted " + at
}

// Go forbids a method named like the Session.Title field, and the list's
// DefaultItem needs a Title() method.
type item struct{ Session }

func (i item) Title() string {
	if i.Archived {
		return "[archived] " + i.Session.Title
	}
	return i.Session.Title
}

func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

var (
	resumeKey  = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume"))
	archiveKey = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "archive/unarchive"))
	viewKey    = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "active/all"))
	searchKey  = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search"))
	trashKey   = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "move to trash"))
	trashView  = key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "trash view"))
	restoreKey = key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "in trash, restore"))
	purgeKey   = key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "in trash, purge now"))
	// Help only: while the query has focus, updateSearch reads keys itself.
	fullTextKey = key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "in search, toggle full-text"))
)

// keyMap replaces the list's defaults, whose help omits keys they bind (b, u,
// f, d page; esc quits) and which later milestones bind to other actions.
func keyMap() list.KeyMap {
	km := list.DefaultKeyMap()
	km.PrevPage = key.NewBinding(key.WithKeys("left", "h", "pgup"), key.WithHelp("←/h/pgup", "prev page"))
	km.NextPage = key.NewBinding(key.WithKeys("right", "l", "pgdown"), key.WithHelp("→/l/pgdn", "next page"))
	km.Quit = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
	km.ForceQuit = key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "force quit"))
	km.ShowFullHelp = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help"))
	return km
}

type model struct {
	list         list.Model
	config, data string
	sessions     []Session
	showAll      bool
	trash        bool
	// Set by X until the next key, which purges it only when that key is y.
	purging *Session
	err     error

	input     textinput.Model
	searching bool
	fullText  bool
	rg        string
	// nil rather than empty so that before any full-text run every row shows.
	matches map[string]bool
	// Tags full-text runs so a result that arrives after the query moved on
	// is dropped.
	seq int
	// The exec has to wait until Run has restored the terminal, so Update
	// only records its target here.
	resume *resumeTarget
}

type resumeTarget struct {
	claude string
	cwd    string
	uuid   string
}

func newModel(config, data string, sessions []Session) model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Claude Code sessions"
	l.KeyMap = keyMap()
	l.SetFilteringEnabled(false)
	l.AdditionalShortHelpKeys = func() []key.Binding { return []key.Binding{resumeKey, archiveKey, viewKey, searchKey} }
	l.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{resumeKey, archiveKey, viewKey, searchKey, fullTextKey,
			trashKey, trashView, restoreKey, purgeKey, l.KeyMap.ForceQuit}
	}
	rg, _ := exec.LookPath("rg")
	m := model{list: l, config: config, data: data, sessions: sessions, input: textinput.New(), rg: rg}
	m.refresh()
	return m
}

func (m model) viewSessions() []Session {
	var out []Session
	for _, s := range m.sessions {
		if m.trash && s.Trashed || !m.trash && !s.Trashed && (m.showAll || !s.Archived) {
			out = append(out, s)
		}
	}
	return out
}

func (m *model) refresh() {
	q := strings.ToLower(m.input.Value())
	var items []list.Item
	for _, s := range m.viewSessions() {
		if m.fullText && m.matches != nil && !m.matches[s.UUID] ||
			!m.fullText && !matchesFilter(s, q) {
			continue
		}
		items = append(items, item{s})
	}
	idx := m.list.Index()
	m.list.SetItems(items)
	m.list.Select(max(0, min(idx, len(items)-1)))
}

// record keeps moved even when err is set: restore can move the session and
// then fail to remove its sidecar, and the model must follow where it went.
func (m *model) record(s, moved Session, err error) error {
	for i := range m.sessions {
		if m.sessions[i].Path == s.Path {
			m.sessions[i] = moved
		}
	}
	m.refresh()
	return err
}

func (m *model) setArchived(s Session, archived bool) error {
	moved, err := setArchived(s, archived, m.config, m.data)
	return m.record(s, moved, err)
}

func (m *model) purge(s Session) error {
	if err := purge(s, m.config, m.data); err != nil {
		return err
	}
	m.sessions = slices.DeleteFunc(m.sessions, func(o Session) bool { return o.Path == s.Path })
	m.refresh()
	return nil
}

func (m model) Init() tea.Cmd { return nil }

func (m model) filtered() bool { return m.input.Value() != "" || m.matches != nil }

func (m *model) resetSearch(fullText bool) {
	m.seq++
	m.fullText = fullText
	m.matches = nil
	m.refresh()
}

type fullTextMsg struct {
	seq   int
	paths []string
	err   error
}

func (m model) runFullText() tea.Cmd {
	seq, rg, q := m.seq, m.rg, m.input.Value()
	var paths []string
	for _, s := range m.viewSessions() {
		paths = append(paths, s.Path)
	}
	return func() tea.Msg {
		r := fullTextMsg{seq: seq}
		if rg != "" {
			r.paths, r.err = rgSearch(rg, q, paths)
		} else {
			r.paths, r.err = scanSearch(q, paths)
		}
		return r
	}
}

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.searching = false
		m.input.Blur()
		m.input.Reset()
		m.resetSearch(m.fullText)
		return m, nil
	case tea.KeyCtrlF:
		m.resetSearch(!m.fullText)
		return m, nil
	case tea.KeyEnter:
		m.searching = false
		m.input.Blur()
		if m.fullText {
			m.seq++
			return m, m.runFullText()
		}
		return m, nil
	case tea.KeyCtrlC:
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refresh()
	return m, cmd
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// One line for the search prompt and one for an error.
		m.list.SetSize(msg.Width, msg.Height-2)
	case fullTextMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		m.matches = map[string]bool{}
		for _, p := range msg.paths {
			m.matches[strings.TrimSuffix(filepath.Base(p), ".jsonl")] = true
		}
		m.err = msg.err
		m.refresh()
		return m, nil
	case tea.KeyMsg:
		m.err = nil
		if m.searching {
			return m.updateSearch(msg)
		}
		if s := m.purging; s != nil {
			m.purging = nil
			if msg.String() == "y" {
				m.err = m.purge(*s)
			}
			return m, nil
		}
		it, selected := m.list.SelectedItem().(item)
		switch {
		case key.Matches(msg, searchKey):
			m.searching = true
			return m, m.input.Focus()
		case msg.Type == tea.KeyEsc && m.filtered():
			m.input.Reset()
			m.resetSearch(m.fullText)
			return m, nil
		case msg.Type == tea.KeyEsc && m.trash:
			m.trash = false
			m.list.Title = "Claude Code sessions"
			m.refresh()
			return m, nil
		case key.Matches(msg, trashView) && !m.trash:
			// Full-text matches were computed over the view being left.
			m.input.Reset()
			m.trash = true
			m.list.Title = "Trash"
			m.resetSearch(m.fullText)
			return m, nil
		case m.trash:
			// No later case runs in the trash view, so d, a, tab and enter do nothing.
			switch {
			case key.Matches(msg, restoreKey) && selected:
				moved, err := restore(it.Session, m.config, m.data)
				m.err = m.record(it.Session, moved, err)
				return m, nil
			case key.Matches(msg, purgeKey) && selected:
				s := it.Session
				m.purging = &s
				return m, nil
			}
		case key.Matches(msg, trashKey) && selected:
			moved, err := trash(it.Session, m.config, m.data)
			m.err = m.record(it.Session, moved, err)
			return m, nil
		case key.Matches(msg, viewKey):
			m.showAll = !m.showAll
			m.refresh()
			return m, nil
		case key.Matches(msg, archiveKey) && selected:
			m.err = m.setArchived(it.Session, !it.Archived)
			return m, nil
		case key.Matches(msg, resumeKey) && selected:
			t, err := prepareResume(it.Session)
			if err == nil {
				// Claude's --resume finds only sessions under its own projects dir.
				err = m.setArchived(it.Session, false)
			}
			if err != nil {
				m.err = err
				return m, nil
			}
			m.resume = &t
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	v := m.list.View()
	if m.searching || m.filtered() {
		in := m.input
		switch {
		case !m.fullText:
			in.Prompt = "filter: "
		case m.rg != "":
			in.Prompt = "full-text (rg): "
		default:
			in.Prompt = "full-text (scan): "
		}
		v += "\n" + in.View()
	}
	if m.purging != nil {
		v += fmt.Sprintf("\npurge %s for good? y/n", m.purging.Title)
	}
	if m.err != nil {
		v += "\nerror: " + m.err.Error()
	}
	return v
}

// prepareResume checks what the exec needs while the TUI can still show why
// it cannot happen.
func prepareResume(s Session) (resumeTarget, error) {
	if s.Cwd == "" {
		return resumeTarget{}, fmt.Errorf("session %s records no cwd", s.UUID)
	}
	if fi, err := os.Stat(s.Cwd); err != nil {
		return resumeTarget{}, err
	} else if !fi.IsDir() {
		return resumeTarget{}, fmt.Errorf("%s is not a directory", s.Cwd)
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		return resumeTarget{}, err
	}
	return resumeTarget{claude: claude, cwd: s.Cwd, uuid: s.UUID}, nil
}
