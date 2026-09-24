<!-- orc:begin -->
This repo's work is tracked in orc, a task tree with a command line tool of the
same name. Look there before you start.
<!-- orc:end -->

# Layout

One package, `main`, at the repository root, split into four parts by file.
Every non-test `.go` file belongs to exactly one of them.

| Part          | Files                                                                                      | Holds                                                         |
|---------------|--------------------------------------------------------------------------------------------|---------------------------------------------------------------|
| session store | `sessions.go`, `archive.go`, `trash.go`, `search.go`, `rename_darwin.go`, `rename_linux.go`, `rename_windows.go`, `pid_unix.go`, `pid_windows.go` | discovery and parsing; moves and the liveness check; trash, restore, purge; filter and full-text search; the per-OS rename and PID probes |
| TUI           | `tui.go`                                                                                   | the bubbletea model and its key bindings                      |
| CLI           | `cli.go`                                                                                   | the cobra commands                                            |
| process       | `main.go`, `resume.go`, `resume_unix.go`, `resume_windows.go`                              | entry and TUI start; the resume checks in `prepareResume`, and running `claude` |

The session store files import neither `github.com/charmbracelet/...` packages
nor `github.com/spf13/cobra`. They are plain filesystem code; a UI concern
that leaks into them belongs in `tui.go` or `cli.go` instead.

Which part may call into which:

- **session store** calls nothing outside the session store.
- **TUI** calls the session store, and from the process part only
  `prepareResume` (and its `resumeTarget`). It never runs `resume`: the model
  records the target, and `main.go` runs it after the program exits and the
  terminal is restored.
- **CLI** calls the session store, the TUI model's `viewSessions` (so `ls`
  lists by the same scope rule as the TUI), and the process part (`run`,
  `prepareResume`, `resume`).
- **process**: `main.go` calls the CLI (`rootCmd`), the TUI (`newModel` and the final `model`),
  the session store, and `resume`. `resume.go` and `resume_*.go` call only the session store
  (the `Session` type), never the TUI or the CLI.

Nothing else is allowed; in particular the TUI never calls the CLI.
