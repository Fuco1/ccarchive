package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ccarchive",
		Short: "Archive, trash and resume Claude Code sessions",
		Args:  cobra.NoArgs,
		// An operation's error is not a usage mistake, so it is not followed by
		// the usage text.
		SilenceUsage: true,
		RunE:         func(*cobra.Command, []string) error { return run() },
	}
	root.SetErrPrefix("ccarchive:")
	root.AddCommand(lsCmd(),
		sessionCmd("archive", "Archive a session", false, func(s Session, config, data string) error {
			_, err := setArchived(s, true, config, data)
			return err
		}),
		sessionCmd("unarchive", "Unarchive a session", false, func(s Session, config, data string) error {
			_, err := setArchived(s, false, config, data)
			return err
		}),
		sessionCmd("trash", "Move a session to the trash", false, func(s Session, config, data string) error {
			_, err := trash(s, config, data)
			return err
		}),
		sessionCmd("restore", "Restore a session from the trash", true, func(s Session, config, data string) error {
			_, err := restore(s, config, data)
			return err
		}),
		sessionCmd("purge", "Delete a trashed session for good", true, purge),
		sessionCmd("resume", "Resume a session in Claude Code", false, func(s Session, config, data string) error {
			t, err := prepareResume(s)
			if err != nil {
				return err
			}
			// Claude's --resume finds only sessions under its own projects dir.
			if _, err := setArchived(s, false, config, data); err != nil {
				return err
			}
			return resume(t)
		}),
	)
	return root
}

func load() (config, data string, sessions []Session, err error) {
	if config, err = configDir(); err != nil {
		return
	}
	if data, err = dataDir(); err != nil {
		return
	}
	sessions, err = listSessions(config, data)
	return
}

// inTrash mirrors the TUI, where restore and purge are keys of the trash view
// only and every other session key is inert there.
func sessionCmd(name, short string, inTrash bool, op func(s Session, config, data string) error) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			config, data, sessions, err := load()
			if err != nil {
				return err
			}
			s, err := resolve(sessions, args[0])
			if err != nil {
				return err
			}
			if s.Trashed != inTrash {
				if s.Trashed {
					return fmt.Errorf("session %s is in the trash; restore it first", s.UUID)
				}
				return fmt.Errorf("session %s is not in the trash", s.UUID)
			}
			return op(s, config, data)
		},
	}
}

// An empty id would be a prefix of every UUID, so it is refused rather than
// resolving whenever exactly one session exists.
func resolve(sessions []Session, id string) (Session, error) {
	if id == "" {
		return Session{}, fmt.Errorf("empty session id")
	}
	var hits []string
	var found Session
	for _, s := range sessions {
		if strings.HasPrefix(s.UUID, id) {
			hits = append(hits, s.UUID)
			found = s
		}
	}
	switch len(hits) {
	case 0:
		return Session{}, fmt.Errorf("no session matches %s", id)
	case 1:
		return found, nil
	}
	return Session{}, fmt.Errorf("%s matches %d sessions:\n%s", id, len(hits), strings.Join(hits, "\n"))
}

func lsCmd() *cobra.Command {
	var all, archived, trashed bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List this directory's sessions: UUID, state, cwd and title, tab-separated",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _, sessions, err := load()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			m := model{sessions: sessions, cwd: filepath.Clean(cwd), allProjects: all, showAll: archived, trash: trashed}
			for _, s := range m.viewSessions() {
				state := "active"
				if s.Trashed {
					state = "trashed"
				} else if s.Archived {
					state = "archived"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", s.UUID, state, s.Cwd, s.Title)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "list every project's sessions")
	cmd.Flags().BoolVar(&archived, "archived", false, "include archived sessions")
	cmd.Flags().BoolVar(&trashed, "trash", false, "list the trash instead")
	return cmd
}
