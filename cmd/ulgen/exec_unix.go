//go:build !windows

package main

import (
	"context"
	osexec "os/exec"
	"os/signal"
	"syscall"
)

// platformExec replaces Ülgen with the tool, so the terminal and the exit
// code are the tool's own. The arguments reach the tool exactly as given: no
// shell reads them.
//
// Ülgen asked to be told about termination signals (main's signalContext);
// that registration must go before the tool takes over, or a signal the
// tool's own caller had ignored — an inherited ignore, say — would reach the
// tool as one Ülgen was catching. Resetting the same signals main registered
// gives each back the disposition it had, so an ignored signal stays ignored
// in the tool. It returns only when the tool could not be started.
func platformExec(binary string, argv, env []string) (int, error) {
	signal.Reset(terminationSignals...)
	return 0, syscall.Exec(binary, argv, env)
}

// toolCommand prepares running a tool as a child process, which is how its
// version is asked for. On Unix that needs nothing special: the arguments go
// to the program as they are.
func toolCommand(ctx context.Context, binary string, args []string) (*osexec.Cmd, error) {
	return osexec.CommandContext(ctx, binary, args...), nil
}
