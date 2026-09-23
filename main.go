package main

import (
	"fmt"
	"os"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ccarchive:", err)
		os.Exit(1)
	}
}

func run() error {
	config, err := configDir()
	if err != nil {
		return err
	}
	data, err := dataDir()
	if err != nil {
		return err
	}
	sessions, err := listSessions(config, data)
	if err != nil {
		return err
	}
	final, err := tea.NewProgram(newModel(config, data, sessions), tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	t := final.(model).resume
	if t == nil {
		return nil
	}
	// Run has restored the terminal by now; the operator runs each resumed
	// session in its own tmux window, so ccarchive does not come back.
	if err := os.Chdir(t.cwd); err != nil {
		return err
	}
	return syscall.Exec(t.claude, []string{"claude", "--resume", t.uuid}, os.Environ())
}
