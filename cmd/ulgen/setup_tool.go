package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mftnakrsu/ulgen/internal/diff"
	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/setup"
)

// setupRun carries what the steps of one "ulgen setup" share; its methods
// do the work for one tool and print the report.
type setupRun struct {
	ctx            context.Context
	stdout, stderr io.Writer
	env            setup.Env
	tr             *i18n.Translator
	dryRun         bool // --dry-run: show the changes, write nothing
	remove         bool // --remove: undo the setup instead of making it
	getenv         func(string) string
	req            setup.Request // what every tool is set up with
	backups        string        // this run's backup directory
	// key is the EVREN key. Nothing prints it; it is here so that it can be
	// recognised, and hidden, in a file that already holds it.
	key string
}

// setUpAll sets the tools up one after the other. A tool that fails does not
// stop the next one; the result is then a failure without a message of its
// own. A cancelled run stops at once.
func (s *setupRun) setUpAll(tools []candidate) *failure {
	failed := false
	for i, c := range tools {
		if s.ctx.Err() != nil {
			return cancelled(s.tr)
		}
		if c.skip {
			fmt.Fprintln(s.stdout, s.tr.T("setup.skipped", c.tool.Name()))
			continue
		}
		if i > 0 {
			fmt.Fprintln(s.stdout)
		}
		if !c.detected {
			c.found = c.tool.Detect(s.ctx, s.env)
		}
		f := s.setUpTool(c.tool, c.found)
		switch {
		case f == nil:
		case f.code == exitInterrupted:
			return f
		default:
			f.report(s.stderr, "")
			failed = true
		}
	}
	if failed {
		return &failure{code: exitError}
	}
	return nil
}

// setUpTool plans, shows and, unless this is a dry run, applies one tool's
// setup. The context is checked once more right before the files are
// written: a Ctrl+C during the steps before must not be followed by a write.
func (s *setupRun) setUpTool(tool setup.Tool, found setup.Detection) *failure {
	// The tool's header names it and its version; both come from the probe
	// that just ran. A Ctrl+C during that probe must not be followed by a
	// header: it would name a tool as if its turn had begun, and then
	// "cancelled" would contradict it.
	if s.ctx.Err() != nil {
		return cancelled(s.tr)
	}
	s.printHeader(tool, found)
	var plan setup.Plan
	var err error
	if s.remove {
		plan, err = tool.Remove(s.env)
	} else {
		plan, err = tool.Plan(s.env, s.req)
	}
	if err != nil {
		return &failure{msg: describeSetupError(err, s.env.Home, s.tr), code: exitError}
	}
	if s.ctx.Err() != nil {
		return cancelled(s.tr)
	}
	if s.dryRun {
		for _, f := range plan.Files {
			io.WriteString(s.stdout, s.diffOf(f))
		}
		// Predict the refusals a real run would hit, so the dry run does not
		// promise a write that would be refused.
		if err := setup.Check(plan); err != nil {
			return &failure{msg: describeSetupError(err, s.env.Home, s.tr), code: exitError}
		}
	} else if f := s.apply(tool, plan); f != nil {
		return f
	}
	s.printOutcome(tool, found, plan)
	return nil
}

// apply writes a plan and lists the files it wrote. After a failure it still
// lists the files written before it, with their backups, so the user can see
// and undo what happened.
func (s *setupRun) apply(tool setup.Tool, plan setup.Plan) *failure {
	err := setup.Apply(plan, tool.ID(), s.backups)
	if err == nil {
		s.printWritten(plan.Files, tool.ID())
		return nil
	}
	var write *setup.WriteError
	if errors.As(err, &write) && len(write.Done) <= len(plan.Files) {
		s.printWritten(plan.Files[:len(write.Done)], tool.ID())
	}
	return &failure{msg: describeSetupError(err, s.env.Home, s.tr), code: exitError}
}

// printHeader prints the tool's name with its version, or says that it is
// not installed.
func (s *setupRun) printHeader(tool setup.Tool, found setup.Detection) {
	switch {
	case !found.Installed:
		fmt.Fprintln(s.stdout, s.tr.T("setup.header.missing", tool.Name()))
	case found.Version == "":
		fmt.Fprintln(s.stdout, tool.Name())
	default:
		fmt.Fprintln(s.stdout, tool.Name()+" "+oneLine(found.Version))
	}
}

// printWritten lists files that were written: created, or updated with the
// size of the change and where the backup is. A file that held the key in
// clear text gets one more line. When the new file no longer holds the key,
// the line is about the backup that still does; when the new file still holds
// it — under a name Ülgen does not touch — the line says so and names the
// lines, because Ülgen does not remove the key.
func (s *setupRun) printWritten(files []setup.FileChange, toolID string) {
	for _, f := range files {
		path := showPath(s.env.Home, f.Path)
		if f.Delete {
			fmt.Fprintf(s.stdout, "  %s  %s\n", path, s.tr.T("setup.file.removed"))
			fmt.Fprintf(s.stdout, "    %s\n", s.tr.T("setup.backup", showPath(s.env.Home, setup.BackupPath(s.backups, toolID, f.Path))))
			continue
		}
		if f.Old == nil {
			fmt.Fprintf(s.stdout, "  %s  %s\n", path, s.tr.T("setup.file.created"))
			continue
		}
		added, removed := diff.Stat(string(f.Old), string(f.New))
		backup := showPath(s.env.Home, setup.BackupPath(s.backups, toolID, f.Path))
		fmt.Fprintf(s.stdout, "  %s  %s\n", path, s.tr.T("setup.file.updated", added, removed))
		fmt.Fprintf(s.stdout, "    %s\n", s.tr.T("setup.backup", backup))
		if s.key == "" || !bytes.Contains(f.Old, []byte(s.key)) {
			continue
		}
		if lines := keyLines(f.New, s.key); len(lines) > 0 {
			key := "setup.file_holds_key"
			if len(lines) > 1 {
				key = "setup.file_holds_key_lines"
			}
			fmt.Fprintf(s.stdout, "    %s\n", s.tr.T(key, lineNumbers(lines)))
			continue
		}
		fmt.Fprintf(s.stdout, "    %s\n", s.tr.T("setup.backup_holds_key", backup))
	}
}

// keyLines returns the numbers, counting from 1, of the lines of text that
// hold the key.
func keyLines(text []byte, key string) []int {
	var lines []int
	for i, line := range strings.Split(string(text), "\n") {
		if strings.Contains(line, key) {
			lines = append(lines, i+1)
		}
	}
	return lines
}

// lineNumbers writes a list of line numbers for a message: "3" or "3, 7".
func lineNumbers(lines []int) string {
	parts := make([]string, len(lines))
	for i, n := range lines {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

// printOutcome prints what follows the list of files: that nothing changed
// or nothing was written, the notes, and how to start the tool. While the
// changes are only planned (a dry run that would change something), the
// lines about starting the tool are left out and a note about a change says
// "would" (see noteLine).
func (s *setupRun) printOutcome(tool setup.Tool, found setup.Detection, plan setup.Plan) {
	planned := s.dryRun && len(plan.Files) > 0
	switch {
	case s.remove && len(plan.Files) == 0:
		fmt.Fprintln(s.stdout, "  "+s.tr.T("setup.remove.nothing"))
	case len(plan.Files) == 0:
		fmt.Fprintln(s.stdout, "  "+s.tr.T("setup.up_to_date"))
	case s.dryRun:
		fmt.Fprintln(s.stdout, "  "+s.tr.T("setup.dry_run"))
	}
	notes := append(append([]setup.Note{}, found.Notes...), plan.Notes...)
	if s.remove {
		notes = plan.Notes // nothing about installing or running the tool
	} else if !found.Installed {
		notes = append(notes, tool.InstallHint(s.env))
	}
	for _, n := range notes {
		if line, ok := noteLine(s.tr, s.env, n, planned); ok {
			fmt.Fprintln(s.stdout, "  "+line)
		}
	}
	switch {
	case planned:
	case !s.remove:
		fmt.Fprintln(s.stdout, "  "+s.tr.T("setup.start", tool.ID()))
	case len(plan.Files) > 0:
		fmt.Fprintln(s.stdout, "  "+s.tr.T("setup.remove.done", tool.Name()))
		// "ulgen" alone would set the tool up again on its next run.
		if readGuideTool(s.getenv) == tool.ID() {
			forgetGuideTool(s.getenv)
		}
	}
}

// list prints one line per tool: installed or not, set up or not. A
// cancelled run prints nothing: a table of what was guessed after the
// interrupt would look like an answer.
func (s *setupRun) list() int {
	rows := [][3]string{{s.tr.T("setup.list.tool"), s.tr.T("setup.list.installed"), "EVREN"}}
	for _, tool := range setup.Tools() {
		if s.ctx.Err() != nil {
			break
		}
		rows = append(rows, s.listRow(tool))
	}
	if s.ctx.Err() != nil {
		return cancelled(s.tr).report(s.stderr, "")
	}
	widths := [2]int{}
	for _, r := range rows {
		for i := range widths {
			widths[i] = max(widths[i], utf8.RuneCountInString(r[i]))
		}
	}
	for _, r := range rows {
		fmt.Fprintf(s.stdout, "%-*s   %-*s   %s\n", widths[0], r[0], widths[1], r[1], r[2])
	}
	fmt.Fprintln(s.stdout)
	fmt.Fprintln(s.stdout, s.tr.T("setup.list.hint"))
	return exitOK
}

// listRow is one tool's line of the list: its id, the installed version (or
// "yes" when the version is unknown, or "not found"), and the state of
// EVREN in it (listState).
func (s *setupRun) listRow(tool setup.Tool) [3]string {
	installed := s.tr.T("setup.list.not_found")
	if found := tool.Detect(s.ctx, s.env); found.Installed {
		installed = oneLine(found.Version)
		if installed == "" {
			installed = s.tr.T("setup.list.found")
		}
	}
	return [3]string{tool.ID(), installed, s.tr.T(listState(tool.Configured(s.env)))}
}

// listState returns the message key for what Configured said about a tool:
// set up, not set up, or one of two kinds of trouble. "Needs attention" is
// for a file that can be read and that setup refuses as it is: an entry of
// the user's own under EVREN's name, a profile selected in the old way, a
// key of another shape than the tool allows. "Its file cannot be read" is
// for a file that does not parse or cannot be opened. In both cases
// "ulgen setup <tool>" gives the full message.
func listState(configured bool, err error) string {
	var (
		foreign *setup.ForeignEntryError
		legacy  *setup.LegacyProfileError
		shape   *setup.ShapeError
	)
	switch {
	case errors.As(err, &foreign), errors.As(err, &legacy), errors.As(err, &shape):
		return "setup.list.attention"
	case err != nil:
		return "setup.list.unreadable"
	case configured:
		return "setup.list.set_up"
	}
	return "setup.list.not_set_up"
}
