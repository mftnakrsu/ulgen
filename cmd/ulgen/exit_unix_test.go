//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A child ended by a signal gives 128 plus the signal's number on Unix.
func TestChildKilledBySignalExitCode(t *testing.T) {
	code, err := runChild("/bin/sh", []string{"/bin/sh", "-c", "kill -9 $$"}, []string{"PATH=/bin"})
	if err != nil {
		t.Fatalf("runChild: %v", err)
	}
	if code != 137 {
		t.Errorf("code = %d, want 137 (128 + SIGKILL)", code)
	}
}

// processGone reports whether the process pid no longer exists. Signal 0
// sends nothing; it only asks.
func processGone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// readPid reads the process id a script wrote to path, or 0.
func readPid(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return 0
	}
	return pid
}

// The worker's child leads a process group of its own, and the deadline's
// termination signal and kill go to the whole group: a child that ignores the
// termination signal and a grandchild that ignores it too are both gone after
// the grace time. The test only signals the processes it started.
func TestWorkerChildEndsWithItsGrandchild(t *testing.T) {
	saved := childKillGrace
	childKillGrace = 300 * time.Millisecond
	t.Cleanup(func() { childKillGrace = saved })

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	script := `trap '' TERM
sh -c 'trap "" TERM; echo $$ > "$1"; exec sleep 60' sh "$1" &
while [ ! -s "$1" ]; do sleep 0.05; done
wait`

	stop := make(chan struct{})
	type result struct {
		end   workerEnd
		cause *endCause
	}
	done := make(chan result, 1)
	go func() {
		end, cause := runChildCaptured(context.Background(), "/bin/sh",
			[]string{"sh", "-c", script, "sh", pidFile},
			[]string{"PATH=/usr/bin:/bin"}, t.TempDir(), strings.NewReader(""), os.Stderr, stop)
		done <- result{end, cause}
	}()

	// Wait until the grandchild has recorded itself, then reach the deadline.
	var pid int
	for deadline := time.Now().Add(10 * time.Second); pid == 0 && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
		pid = readPid(pidFile)
	}
	if pid == 0 {
		close(stop)
		t.Fatal("the grandchild never recorded its process id")
	}
	// Whatever happens next, the grandchild is one this test started: it is
	// ended by the id it recorded if the assertions below find it alive.
	t.Cleanup(func() {
		if !processGone(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	start := time.Now()
	close(stop)

	var r result
	select {
	case r = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the child was not ended")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("ending took %v", took)
	}
	if sent, intr := r.cause.result(); !sent || intr {
		t.Errorf("cause = sent %v interrupt %v, want the deadline", sent, intr)
	}
	if r.end.code != 128+int(syscall.SIGKILL) {
		t.Errorf("code = %d, want the kill (137)", r.end.code)
	}
	for deadline := time.Now().Add(3 * time.Second); !processGone(pid) && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
	}
	if !processGone(pid) {
		t.Errorf("the grandchild %d is still running after the deadline", pid)
	}
}

// A child that obeys the termination signal may leave behind something it
// started that ignores the signal and no longer holds the output pipe. That
// is ended too, as soon as the child has ended, without waiting for the grace
// time. The test only signals the process it started.
func TestWorkerChildLeavesNothingBehind(t *testing.T) {
	saved := childKillGrace
	childKillGrace = time.Minute // the grace time must not be what ends it
	t.Cleanup(func() { childKillGrace = saved })

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	script := `sh -c 'trap "" TERM; echo $$ > "$1"; exec sleep 60' sh "$1" >/dev/null 2>&1 &
while [ ! -s "$1" ]; do sleep 0.05; done
wait`

	stop := make(chan struct{})
	done := make(chan workerEnd, 1)
	go func() {
		end, _ := runChildCaptured(context.Background(), "/bin/sh",
			[]string{"sh", "-c", script, "sh", pidFile},
			[]string{"PATH=/usr/bin:/bin"}, t.TempDir(), strings.NewReader(""), os.Stderr, stop)
		done <- end
	}()

	var pid int
	for deadline := time.Now().Add(10 * time.Second); pid == 0 && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
		pid = readPid(pidFile)
	}
	if pid == 0 {
		close(stop)
		t.Fatal("the grandchild never recorded its process id")
	}
	t.Cleanup(func() {
		if !processGone(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	close(stop)

	select {
	case end := <-done:
		if end.code != 128+int(syscall.SIGTERM) {
			t.Errorf("code = %d, want the termination signal (143)", end.code)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the child was not ended")
	}
	for deadline := time.Now().Add(3 * time.Second); !processGone(pid) && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
	}
	if !processGone(pid) {
		t.Errorf("the grandchild %d outlived the child", pid)
	}
}

// A child that ends by itself keeps its code and records no signal, even when
// the deadline is reached a moment later.
func TestWorkerChildEndedByItselfRecordsNoSignal(t *testing.T) {
	stop := make(chan struct{})
	end, cause := runChildCaptured(context.Background(), "/bin/sh",
		[]string{"sh", "-c", "echo hi; exit 3"}, []string{"PATH=/usr/bin:/bin"},
		t.TempDir(), strings.NewReader(""), os.Stderr, stop)
	close(stop) // the deadline arrives after the child has ended
	time.Sleep(50 * time.Millisecond)
	if sent, intr := cause.result(); sent || intr {
		t.Errorf("cause = sent %v interrupt %v, want none", sent, intr)
	}
	if end.code != 3 || string(end.out) != "hi\n" || end.err != nil {
		t.Errorf("end = %+v", end)
	}
}

// What a child writes just before it ends is not lost, also when the reader
// is slower than the child: the pipe is read to its end before it is closed.
// (On a busy machine the reader had not started when the child was already
// gone, and the output was thrown away with the pipe.)
func TestWorkerChildOutputIsReadToTheEnd(t *testing.T) {
	saved := beforeChildRead
	beforeChildRead = func() { time.Sleep(100 * time.Millisecond) }
	t.Cleanup(func() { beforeChildRead = saved })

	end, _ := runChildCaptured(context.Background(), "/bin/sh",
		[]string{"sh", "-c", "echo last words; exit 3"}, []string{"PATH=/usr/bin:/bin"},
		t.TempDir(), strings.NewReader(""), os.Stderr, make(chan struct{}))
	if end.err != nil || end.code != 3 || string(end.out) != "last words\n" {
		t.Errorf("end = code %d err %v out %q", end.code, end.err, end.out)
	}
}

// A process the child leaves behind with the output pipe still open does not
// hold the run: after the grace time the pipe is closed and what was written
// until then is kept. The test only signals the process it started.
func TestWorkerChildOutputDoesNotWaitForALeftover(t *testing.T) {
	saved := childOutputGrace
	childOutputGrace = 300 * time.Millisecond
	t.Cleanup(func() { childOutputGrace = saved })

	pidFile := filepath.Join(t.TempDir(), "leftover.pid")
	script := `sh -c 'echo $$ > "$1"; exec sleep 60' sh "$1" &
while [ ! -s "$1" ]; do sleep 0.05; done
echo done`
	start := time.Now()
	end, cause := runChildCaptured(context.Background(), "/bin/sh",
		[]string{"sh", "-c", script, "sh", pidFile}, []string{"PATH=/usr/bin:/bin"},
		t.TempDir(), strings.NewReader(""), os.Stderr, make(chan struct{}))
	pid := readPid(pidFile)
	if pid != 0 {
		t.Cleanup(func() {
			if !processGone(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		})
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Errorf("the run waited %v for the leftover", took)
	}
	if sent, _ := cause.result(); sent || end.code != 0 || string(end.out) != "done\n" {
		t.Errorf("end = %+v, signalled %v", end, sent)
	}
}

// An interrupt (the context) is recorded as an interrupt, and the child is
// ended with the termination signal.
func TestWorkerChildInterruptIsRecorded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	end, cause := runChildCaptured(ctx, "/bin/sh",
		[]string{"sh", "-c", "sleep 60"}, []string{"PATH=/usr/bin:/bin"},
		t.TempDir(), strings.NewReader(""), os.Stderr, make(chan struct{}))
	if sent, intr := cause.result(); !sent || !intr {
		t.Errorf("cause = sent %v interrupt %v, want an interrupt", sent, intr)
	}
	if end.code != 128+int(syscall.SIGTERM) {
		t.Errorf("code = %d, want 143", end.code)
	}
}
