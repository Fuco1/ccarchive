package main

import (
	"os"
	"os/signal"
)

// Windows has no exec, so Claude runs as a child. A console Ctrl+C reaches
// every attached process: signal.Ignore falls through to the default handler,
// which kills ccarchive, and SetConsoleCtrlHandler(nil, true) is inherited, so
// Claude would stop seeing it. Notify must precede the start, or a Ctrl+C in
// between kills the parent.
func resume(t resumeTarget) error {
	signal.Notify(make(chan os.Signal, 1), os.Interrupt)
	code, err := runChild(t.cwd, t.claude, "--resume", t.uuid)
	if err != nil {
		return err
	}
	os.Exit(code)
	return nil
}
