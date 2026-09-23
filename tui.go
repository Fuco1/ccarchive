package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func (s Session) FilterValue() string { return s.Title }
func (s Session) Description() string { return s.Cwd + "  " + age(time.Since(s.ModTime)) }

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
	// sessions holds active and archived alike; the list shows a view of it.
	sessions []Session
	showAll  bool
	err      error
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
	l.AdditionalShortHelpKeys = func() []key.Binding { return []key.Binding{resumeKey, archiveKey, viewKey} }
	l.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{resumeKey, archiveKey, viewKey, l.KeyMap.ForceQuit}
	}
	m := model{list: l, config: config, data: data, sessions: sessions}
	m.refresh()
	return m
}

func (m *model) refresh() {
	var items []list.Item
	for _, s := range m.sessions {
		if m.showAll || !s.Archived {
			items = append(items, item{s})
		}
	}
	idx := m.list.Index()
	m.list.SetItems(items)
	m.list.Select(max(0, min(idx, len(items)-1)))
}

// setArchived moves s and records its new place in the model.
func (m *model) setArchived(s Session, archived bool) error {
	moved, err := setArchived(s, archived, m.config, m.data)
	if err != nil {
		return err
	}
	for i := range m.sessions {
		if m.sessions[i].Path == s.Path {
			m.sessions[i] = moved
		}
	}
	m.refresh()
	return nil
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height-1)
	case tea.KeyMsg:
		m.err = nil
		it, selected := m.list.SelectedItem().(item)
		switch {
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
