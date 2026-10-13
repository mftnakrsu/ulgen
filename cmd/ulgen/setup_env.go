package main

import (
	"context"
	"errors"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/mftnakrsu/ulgen/internal/setup"
)

// This file is where "ulgen setup" and "ulgen run" meet the real machine:
// the home directory, PATH, the version probe and the backup directory.

// versionProbeTimeout bounds "<binary> --version". versionProbeGrace is how
// much longer Ülgen then waits for the program's output: a child the program
// started may still hold the pipe open after the program itself has ended or
// was stopped, and without a limit the wait would last as long as that child
// lives. They are variables so tests can shorten them.
var (
	versionProbeTimeout = 5 * time.Second
	versionProbeGrace   = 500 * time.Millisecond
)

// setupEnv builds the adapters' view of the real machine. Tests replace it.
var setupEnv = realSetupEnv

// realSetupEnv describes the machine Ülgen runs on. An unknown home
// directory is left empty here and refused by checkedEnv.
func realSetupEnv(getenv func(string) string) setup.Env {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return setup.Env{
		GOOS:     runtime.GOOS,
		Home:     home,
		Getenv:   getenv,
		LookPath: lookPathAbs,
		Version:  probeVersion,
	}
}

// checkedEnv returns the adapters' view of the machine, or the reason it
// cannot be used: an unknown or relative home directory, or a variable that
// moves a tool's directory, or Ülgen's own, to a relative path. Both
// commands call it before they read or start anything, because every path
// they use is built from these values and a relative one would mean the
// current directory, where a cloned repository can plant files.
func checkedEnv(getenv func(string) string) (setup.Env, error) {
	env := setupEnv(getenv)
	if err := env.Validate(); err != nil {
		return env, err
	}
	return env, checkConfigDirVar(getenv)
}

// lookPathAbs finds a program like os/exec.LookPath and adds one promise:
// the result is an absolute path. LookPath itself refuses what it finds
// through a relative PATH entry (ErrDot), but it returns a name that
// contains a separator as it was given; here that is refused too.
func lookPathAbs(name string) (string, error) {
	path, err := osexec.LookPath(name)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		return "", &osexec.Error{Name: name, Err: osexec.ErrDot}
	}
	return path, nil
}

// probeVersion runs "<binary> --version" and returns what it printed. It
// takes versionProbeTimeout at most, plus versionProbeGrace for the output.
func probeVersion(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()
	cmd, err := toolCommand(ctx, binary, []string{"--version"})
	if err != nil {
		return "", err
	}
	if isBatchFile(binary) {
		// The probe runs the same cmd.exe start as a real one, so it needs
		// the same environment (see withNoDefaultCurrentDir).
		cmd.Env = withNoDefaultCurrentDir(environ())
	}
	cmd.WaitDelay = versionProbeGrace
	out, err := cmd.Output()
	// The program answered and ended; only something it left behind still
	// held the pipe. The answer counts.
	if errors.Is(err, osexec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		err = nil
	}
	return string(out), err
}

// noVersion stands in for the version probe where only the path of a tool
// is needed: "ulgen run" would otherwise run the tool twice.
func noVersion(context.Context, string) (string, error) { return "", nil }

// backupDir is where this run's backups go:
// <Ülgen configuration directory>/backups/<date-time>. A directory that is
// already there belongs to an earlier run in the same second, so this run
// takes the next free name (<date-time>-2, -3, ...): a backup is never
// written over another one.
func backupDir(getenv func(string) string) (string, error) {
	dir, err := configDir(getenv)
	if err != nil {
		return "", err
	}
	base := filepath.Join(dir, "backups", now().In(localZone()).Format("20060102-150405"))
	path := base
	for n := 2; ; n++ {
		_, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
		path = base + "-" + strconv.Itoa(n)
	}
}
