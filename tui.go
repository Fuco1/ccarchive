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

func (i item) Title() string { return i.Session.Title }

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

var resumeKey = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume"))

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
	list list.Model
	err  error
	// The exec has to wait until Run has restored the terminal, so Update
	// only records its target here.
	resume *resumeTarget
}

type resumeTarget struct {
	claude string
	cwd    string
	uuid   string
}

func newModel(sessions []Session) model {
	items := make([]list.Item, len(sessions))
	for i, s := range sessions {
		items[i] = item{s}
	}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Claude Code sessions"
	l.KeyMap = keyMap()
	l.SetFilteringEnabled(false)
	l.AdditionalShortHelpKeys = func() []key.Binding { return []key.Binding{resumeKey} }
	l.AdditionalFullHelpKeys = func() []key.Binding { return []key.Binding{resumeKey, l.KeyMap.ForceQuit} }
	return model{list: l}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height-1)
	case tea.KeyMsg:
		m.err = nil
		if key.Matches(msg, resumeKey) {
			it, ok := m.list.SelectedItem().(item)
			if !ok {
				return m, nil
			}
			t, err := prepareResume(it.Session)
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
