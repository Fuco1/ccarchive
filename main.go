package main

import (
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if rootCmd().Execute() != nil {
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
	// A failed purge is shown rather than fatal: the trash would otherwise
	// lock the operator out of every other session.
	sessions, purgeErr := purgeExpired(sessions, config, data, time.Now())
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	m := newModel(config, data, cwd, sessions)
	m.err = purgeErr
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	t := final.(model).resume
	if t == nil {
		return nil
	}
	// Only after Run returns: before that the terminal is in raw mode on the
	// alternate screen, and Claude would inherit it that way.
	return resume(*t)
}
