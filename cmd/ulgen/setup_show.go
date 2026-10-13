package main

import (
	"errors"
	"io/fs"
	"strings"
	"unicode/utf8"

	"github.com/mftnakrsu/ulgen/internal/diff"
	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/setup"
	"github.com/mftnakrsu/ulgen/internal/termsafe"
)

// This file turns what "ulgen setup" found into text for the user: notes,
// errors, and the diff of a dry run. Everything that comes from a file, from
// another program or from the command line passes through here before it is
// printed.

// startNotes are the notes about starting a tool by its own name. They are
// left out while the setup is only planned: there is nothing to start yet.
var startNotes = map[string]bool{
	"setup.note.export":        true,
	"setup.note.codex_profile": true,
}

// plannedNotes maps a note that reports a change to its wording for a change
// that was not made (--dry-run).
var plannedNotes = map[string]string{
	"setup.note.stats_off":     "setup.note.stats_off_planned",
	"setup.note.skill_foreign": "setup.note.skill_foreign_planned",
}

// noteLine returns the text of a note. planned says that the changes were
// not written; ok is false for a note that is left out then.
func noteLine(tr *i18n.Translator, env setup.Env, n setup.Note, planned bool) (line string, ok bool) {
	if planned {
		if startNotes[n.Key] {
			return "", false
		}
		if key, has := plannedNotes[n.Key]; has {
			n.Key = key
		}
	}
	return renderNote(tr, env, n), true
}

// renderNote translates a note from an adapter. A path among its arguments
// is shown like every other path, and any other text is put on one line
// without escape sequences.
func renderNote(tr *i18n.Translator, env setup.Env, n setup.Note) string {
	args := make([]any, len(n.Args))
	for i, a := range n.Args {
		switch v := a.(type) {
		case setup.Path:
			args[i] = showPath(env.Home, string(v))
		case string:
			args[i] = oneLine(v)
		default:
			args[i] = a
		}
	}
	return tr.T(n.Key, args...)
}

// describeSetupError turns an error of the environment check, of an adapter
// or of Apply into the message shown to the user. Paths are shown as in the
// report lines, and text that comes from a file or from the operating system
// is put on one line inside a translated sentence.
func describeSetupError(err error, home string, tr *i18n.Translator) string {
	if msg, ok := describeEnvError(err, tr); ok {
		return msg
	}
	var (
		parse   *setup.ParseError
		shape   *setup.ShapeError
		foreign *setup.ForeignEntryError
		clash   *setup.DefaultClashError
		legacy  *setup.LegacyProfileError
		missing *setup.MissingDirError
		kind    *setup.KindError
		request *setup.RequestError
		write   *setup.WriteError
		file    *fs.PathError
	)
	switch {
	case errors.As(err, &parse):
		return tr.T("setup.err.parse", showPath(home, parse.Path), parse.Format, oneLine(parse.Detail))
	case errors.As(err, &shape):
		return tr.T("setup.err.shape", showPath(home, shape.Path), oneLine(shape.Detail))
	case errors.As(err, &foreign):
		return tr.T("setup.err.foreign", showPath(home, foreign.Path), oneLine(foreign.Entry))
	case errors.As(err, &clash):
		where := showPath(home, clash.Path)
		if clash.Var != "" {
			where = oneLine(clash.Var)
		}
		return tr.T("setup.err.default_clash", where, oneLine(clash.Model))
	case errors.As(err, &legacy):
		return tr.T("setup.err.legacy_profile", showPath(home, legacy.Path), oneLine(legacy.Key))
	case errors.As(err, &missing):
		return tr.T("setup.err.missing_dir", missing.Var, showPath(home, missing.Path))
	case errors.As(err, &kind):
		return describeKindError(kind, home, tr)
	case errors.As(err, &request):
		return tr.T("setup.err.request", oneLine(request.Problem))
	case errors.As(err, &write):
		return describeWriteError(write, home, tr)
	case errors.As(err, &file):
		return tr.T("setup.err.read", showPath(home, file.Path), oneLine(file.Err.Error()))
	}
	return tr.T("setup.err.other", oneLine(err.Error()))
}

// describeKindError words a path that holds another kind of thing than a
// tool keeps there: a directory in the place of a file, or something that
// is no directory where one belongs, named by a variable or not.
func describeKindError(kind *setup.KindError, home string, tr *i18n.Translator) string {
	switch {
	case kind.Var != "":
		return tr.T("setup.err.var_not_dir", kind.Var, showPath(home, kind.Path))
	case kind.WantDir:
		return tr.T("setup.err.not_dir", showPath(home, kind.Path))
	}
	return tr.T("setup.err.is_dir", showPath(home, kind.Path))
}

// describeWriteError words a failed write: why the file was not written
// and, when the plan had written other files before it, which ones. When a
// backup was made before the write failed, one more sentence names it.
func describeWriteError(write *setup.WriteError, home string, tr *i18n.Translator) string {
	var (
		changed  *setup.ChangedError
		readOnly *setup.ReadOnlyError
		link     *setup.LinkError
		backup   *setup.BackupExistsError
		msg      string
	)
	switch {
	case errors.As(write.Err, &changed):
		msg = tr.T("setup.err.changed", showPath(home, write.Path))
	case errors.As(write.Err, &readOnly):
		msg = tr.T("setup.err.read_only", showPath(home, write.Path))
	case errors.As(write.Err, &link):
		msg = tr.T("setup.err.link", showPath(home, link.Path))
	case errors.As(write.Err, &backup):
		msg = tr.T("setup.err.backup_exists", showPath(home, backup.Path), showPath(home, backup.Backup))
	default:
		msg = tr.T("setup.err.write", showPath(home, write.Path), oneLine(write.Err.Error()))
	}
	if write.Backup != "" {
		msg += "; " + tr.T("setup.err.backup_made", showPath(home, write.Backup))
	}
	if len(write.Done) > 0 {
		done := make([]string, len(write.Done))
		for i, path := range write.Done {
			done[i] = showPath(home, path)
		}
		msg += " " + tr.T("setup.err.written", strings.Join(done, ", "))
	}
	return msg
}

// diffOf renders the change to one file for --dry-run. The file is the
// user's and can hold anything, and a diff is what people paste into a bug
// report. So before the two versions are compared and shown, escape
// sequences and control characters are removed, the EVREN key is replaced by
// "[key]" wherever it stands, and a value whose name marks it as a secret is
// replaced by "[hidden]" (maskSecrets).
func (s *setupRun) diffOf(f setup.FileChange) string {
	name := showPath(s.env.Home, f.Path)
	if f.Delete {
		// The whole file goes; its text would only fill the screen.
		return "--- " + name + "\n" + s.tr.T("setup.dry_run_removed") + "\n"
	}
	clean := func(b []byte) string {
		text := termsafe.Sanitize(strings.ToValidUTF8(string(b), string(utf8.RuneError)))
		if s.key != "" {
			text = strings.ReplaceAll(text, s.key, "[key]")
		}
		return keyLike.ReplaceAllString(maskSecrets(text), "[key]")
	}
	text := diff.Unified(name, name, clean(f.Old), clean(f.New))
	if text == "" {
		// The file changes, but only in values that are not shown.
		return "--- " + name + "\n+++ " + name + "\n  " + s.tr.T("setup.dry_run_hidden") + "\n"
	}
	return termsafe.Sanitize(text)
}

// The masking of the diff's text lives in mask.go (maskSecrets).
