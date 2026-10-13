//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	osexec "os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// platformExec runs the tool with Ülgen's own standard streams, waits for it
// and returns its exit code; Windows cannot replace a process.
//
// Ctrl+C and Ctrl+Break go from the console to every process attached to it,
// so the tool receives them by itself and decides what they mean. Ülgen has
// to outlive them to pass the tool's exit code on. It therefore keeps a
// signal registration of its own for as long as the tool runs: main's
// registration ends with the first signal, and a process without any
// registration is ended by Windows at the next one, which would return the
// prompt while the tool is still winding down on the same console and lose
// its exit code. (signal.Ignore would not do: on Windows it removes every
// registration and leaves the default action in place.)
func platformExec(binary string, argv, env []string) (int, error) {
	// The tool runs until it ends by itself: a cancelled context must not
	// stop it, the console's Ctrl+C reaches it directly.
	cmd, err := toolCommand(context.Background(), binary, argv[1:])
	if err != nil {
		return 0, err
	}
	if isBatchFile(binary) {
		// cmd.exe must not look the shim's program up in the current
		// directory (see withNoDefaultCurrentDir).
		env = withNoDefaultCurrentDir(env)
	}
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	interrupts := make(chan os.Signal, 1) // never read: being registered is the point
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	err = cmd.Run()
	var exit *osexec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return 0, err
}

// toolCommand prepares running a tool as a child process: to start it for
// good (platformExec) or to ask for its version. A program (.exe) is started
// directly, with the quoting Go applies for programs that split their
// command line by the C rules. A batch file (.cmd or .bat, which is what npm
// installs) can only be run by cmd.exe, which reads a line by other rules,
// so for it Ülgen starts cmd.exe from the Windows system directory with a
// command line it wrote itself (see shimCommandLine); an argument that
// cannot be passed safely is a *shimArgError and nothing is started.
//
// The batch file npm writes for a tool that runs on node does nothing but
// start "node <script> <arguments>". When the file is that one, to the letter
// (npmShimScript), Ülgen starts node with the script itself: cmd.exe is not
// involved, and no argument has to be refused.
func toolCommand(ctx context.Context, binary string, args []string) (*osexec.Cmd, error) {
	if !isBatchFile(binary) {
		return osexec.CommandContext(ctx, binary, args...), nil
	}
	if node, script, ok := npmShimTarget(binary); ok {
		return osexec.CommandContext(ctx, node, append([]string{script}, args...)...), nil
	}
	line, err := shimCommandLine(binary, args)
	if err != nil {
		return nil, err
	}
	// The system directory comes from Windows itself, not from the
	// SystemRoot or ComSpec variables, which the caller's environment could
	// point anywhere.
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	cmd := osexec.CommandContext(ctx, filepath.Join(system, "cmd.exe"))
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line}
	return cmd, nil
}

// npmShimTarget returns the node program and the script that npm's shim at
// path would start, when the shim is npm's and both are there. Node is the
// copy beside the shim, as in the shim itself, else the node.exe on PATH;
// os/exec does not take a program from the current directory, and a result
// that is not an absolute path is not used.
func npmShimTarget(path string) (node, script string, ok bool) {
	text, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	rel, ok := npmShimScript(text)
	if !ok {
		return "", "", false
	}
	dir := filepath.Dir(path)
	script = filepath.Join(dir, rel)
	if info, err := os.Stat(script); err != nil || !info.Mode().IsRegular() {
		return "", "", false
	}
	node = filepath.Join(dir, "node.exe")
	if info, err := os.Stat(node); err != nil || !info.Mode().IsRegular() {
		if node, err = osexec.LookPath("node.exe"); err != nil || !filepath.IsAbs(node) {
			return "", "", false
		}
	}
	return node, script, true
}
