package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	osexec "os/exec"
	"os/signal"
	"sync"
	"time"
)

// startChild runs a program as a child process of Ülgen, with Ülgen's own
// terminal, waits for it and returns its exit code. It is the seam the tests
// replace, so the command never starts a real tool in a test.
var startChild = runChild

// workerEnd is how a worker's child ended: its code, its captured output, and
// the error that kept it from starting or stopped the reading. Whether the
// deadline or an interrupt ended it is in the endCause returned beside it.
type workerEnd struct {
	code int
	out  []byte
	err  error
}

// startWorkerChild runs a program as the worker's child: standard input from a
// reader, standard output captured, standard error passed through, a working
// directory, and a stop signal that ends it. It is the seam the worker's tests
// replace, so a test never starts a real Claude Code. Keep the behaviour of
// runChild, which "ulgen run claude" uses, unchanged.
var startWorkerChild = runChildCaptured

// maxChildOutput is how much of a worker child's standard output is kept. More
// is an error, not a truncation: the output is a whole JSON document.
const maxChildOutput = 64 << 20

// childKillGrace is how long a child that was asked to end by the termination
// signal is given before it is killed. It is a variable so a test can shorten
// it.
var childKillGrace = 5 * time.Second

// childOutputGrace is how long the reading of a child's output may go on
// after the child has ended, for the case that a process it started still
// holds the pipe. It is a variable so a test can shorten it.
var childOutputGrace = 2 * time.Second

// beforeChildRead runs before each read of a child's output. It does nothing;
// a test makes the reader slow with it, to stand in for a busy machine.
var beforeChildRead = func() {}

// errChildOutputTooLarge is the error of a child whose standard output passed
// maxChildOutput.
var errChildOutputTooLarge = errors.New("the child's output is larger than 64 MB")

// runChild starts binary with argv and env as a child, hands it Ülgen's
// standard streams, waits for it and returns its exit code. It does not
// return before the child has ended: a signal that reaches Ülgen alone is
// forwarded to the child, and one the terminal sends reaches both. A program
// (.exe) is started directly; a batch file goes through the same cmd.exe
// start and refusals as "ulgen run" uses. err is non-nil only when the child
// could not be started or its end could not be read; a child that ends with
// a non-zero code is (code, nil).
func runChild(binary string, argv, env []string) (int, error) {
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

	// A termination signal that reaches Ülgen alone is passed to the child,
	// so it can wind down; the child ends by itself otherwise. The channel
	// is kept registered until the child has ended, so a second signal is
	// forwarded too rather than ending Ülgen while the child still runs.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, terminationSignals...)
	defer signal.Stop(signals)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case sig := <-signals:
				forwardSignal(cmd, sig)
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	err = cmd.Wait()
	var exit *osexec.ExitError
	if errors.As(err, &exit) {
		return exitCodeOf(exit), nil
	}
	return 0, err
}

// runChildCaptured starts binary with argv and env as a child of the worker,
// with stdin as its standard input, the child's standard error passed through
// to stderr as it comes, and its standard output captured, up to
// maxChildOutput: more is an error. dir is the child's working directory. stop
// ends the child when it is closed or when the context is done: a termination
// signal first, and a kill childKillGrace later; on Windows the kill alone. It
// returns the child's exit code and its standard output; err is non-nil only
// when the child could not be started, could not be read, or wrote too much.
//
// The same signal rule as runChild applies: a termination signal that reaches
// Ülgen alone is forwarded to the child, which ends by itself otherwise.
func runChildCaptured(ctx context.Context, binary string, argv, env []string, dir string, stdin io.Reader, stderr io.Writer, stop <-chan struct{}) (workerEnd, *endCause) {
	cmd, err := toolCommand(context.Background(), binary, argv[1:])
	if err != nil {
		return workerEnd{err: err}, nil
	}
	if isBatchFile(binary) {
		env = withNoDefaultCurrentDir(env)
	}
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stderr = stderr
	// The child and everything it starts run in a process group of their own
	// on Unix, so the deadline's signal reaches all of them and the kill cannot
	// miss a grandchild; a child that ignores SIGTERM does not hold the run.
	cmd.SysProcAttr = workerProcessGroup(cmd.SysProcAttr)

	// The output is captured through a pipe of our own, so the bound is
	// enforced as it is read: a child that writes past maxChildOutput is
	// stopped rather than allowed to fill memory or a file.
	pr, pw, err := os.Pipe()
	if err != nil {
		return workerEnd{err: err}, nil
	}
	cmd.Stdout = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		return workerEnd{err: err}, nil
	}
	pw.Close() // the parent keeps only the read end

	// cause records what, if anything, Ülgen sent to end the child. It is
	// filled where the signal is sent and read once the child has ended, so a
	// child that finished by itself keeps its own code even when the deadline
	// or an interrupt arrives at almost the same moment.
	cause := &endCause{}

	var (
		mu       sync.Mutex
		out      bytes.Buffer
		tooLarge bool
	)
	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		buf := make([]byte, 32<<10)
		for {
			beforeChildRead()
			n, rerr := pr.Read(buf)
			if n > 0 {
				mu.Lock()
				if out.Len()+n > maxChildOutput {
					tooLarge = true
					mu.Unlock()
					// The child would block writing to a full pipe; stop it
					// so it can end and Wait can return.
					killChild(cmd)
					return
				}
				out.Write(buf[:n])
				mu.Unlock()
			}
			if rerr != nil {
				return
			}
		}
	}()

	// Signals: the same rule as runChild. A termination signal that reaches
	// Ülgen alone is passed to the child so it can wind down.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, terminationSignals...)
	defer signal.Stop(signals)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case sig := <-signals:
				forwardSignal(cmd, sig)
			}
		}
	}()

	// The deadline or the interrupt ends the child: the termination signal
	// first, then a kill childKillGrace later.
	stopped := make(chan struct{})
	defer close(stopped)
	grace := childKillGrace
	go func() {
		interrupt := false
		select {
		case <-stopped:
			return
		case <-stop:
		case <-ctx.Done():
			interrupt = true
		}
		// When the child has ended and the deadline is reached in the same
		// moment, both channels are ready and the select above may pick
		// either: the child's own end wins, and nothing is sent.
		select {
		case <-stopped:
			return
		default:
		}
		cause.record(interrupt)
		terminateChild(cmd)
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-stopped:
		case <-timer.C:
			killChild(cmd)
		}
	}()

	waitErr := cmd.Wait()
	if sent, _ := cause.result(); sent {
		// The child was told to end and has ended. Anything it started that
		// outlived it (a process that ignores the termination signal and no
		// longer holds the output pipe) goes with the rest of its group.
		sweepChildGroup(cmd)
	}
	// What the child wrote just before it ended may still be in the pipe: the
	// reader is given time to reach the end of it. The end comes at once
	// unless something the child started still holds the pipe open; only then
	// is the pipe closed under the reader.
	select {
	case <-copyDone:
	case <-time.After(childOutputGrace):
	}
	pr.Close()
	<-copyDone

	mu.Lock()
	captured := append([]byte(nil), out.Bytes()...)
	large := tooLarge
	mu.Unlock()

	if large {
		return workerEnd{err: errChildOutputTooLarge}, cause
	}
	var exit *osexec.ExitError
	if errors.As(waitErr, &exit) {
		return workerEnd{code: exitCodeOf(exit), out: captured}, cause
	}
	return workerEnd{out: captured, err: waitErr}, cause
}

// endCause records whether Ülgen itself sent the signal that ended the child,
// and whether the context (an interrupt) rather than the deadline was the
// cause. The worker reads it after the wait, so a timeout or an interrupt is
// reported only when it really reached the child.
type endCause struct {
	mu   sync.Mutex
	sent bool
	intr bool
}

// record notes that the signal was about to be sent; interrupt says the
// context, not the deadline channel, was the reason.
func (c *endCause) record(interrupt bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = true
	c.intr = interrupt
}

// result returns what was recorded so far.
func (c *endCause) result() (sent, interrupt bool) {
	if c == nil {
		return false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sent, c.intr
}

// forwardSignal passes a termination signal on to the child. It is
// best-effort: a system that cannot signal a process this way returns an
// error, which is dropped, because the terminal already reached the child
// where the system delivers signals through a console. A worker's child leads
// a process group (see childSignals), so the signal reaches the whole group.
func forwardSignal(cmd *osexec.Cmd, sig os.Signal) {
	if cmd.Process == nil {
		return
	}
	if signalGroup(cmd, sig) {
		return
	}
	_ = cmd.Process.Signal(sig)
}
