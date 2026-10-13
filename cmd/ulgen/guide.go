package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mftnakrsu/ulgen/internal/credential"
	"github.com/mftnakrsu/ulgen/internal/i18n"
	"github.com/mftnakrsu/ulgen/internal/provider"
	"github.com/mftnakrsu/ulgen/internal/setup"
)

// guideBaseURL is a seam: the tests point the guide's sign-in and setup at a
// stub EVREN. It is empty in the program, which means EVREN itself.
var guideBaseURL = ""

// guideFile is where the guide keeps the tool it started last, in Ülgen's
// configuration directory.
const guideFile = "guide.json"

type guideState struct {
	Tool string `json:"tool"`
}

// runGuide takes someone from nothing to a running tool: "ulgen" alone, and
// "ulgen <tool> [arguments]". It signs in when there is no key, picks the
// tool (the one named, else the one started last, else one of the installed
// ones), sets it up when it is not, and starts it. Each step is the command
// of that name, called with the same arguments a user would type, so the
// guide has no checks of its own and none of theirs is skipped.
func runGuide(ctx context.Context, toolID string, toolArgs []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, tr *i18n.Translator) int {
	// One reader for every question, the sign-in's included, so what one
	// step read ahead is not lost to the next.
	in := bufio.NewReader(stdin)
	var baseURL []string
	if guideBaseURL != "" {
		baseURL = []string{"--base-url", guideBaseURL}
	}

	signedIn, code := guideSignedIn(stderr, getenv, tr)
	if code != exitOK {
		return code
	}
	if !signedIn {
		fmt.Fprintln(stdout, tr.T("guide.login_first"))
		if code := runLogin(ctx, baseURL, in, stdout, stderr, getenv, tr); code != exitOK {
			return code
		}
		fmt.Fprintln(stdout)
	}

	env, err := checkedEnv(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "ulgen: "+describeSetupError(err, env.Home, tr))
		return exitError
	}
	env.Version = noVersion // only the tools' paths are needed here

	tool, code := guideTool(ctx, toolID, env, in, stdout, stderr, getenv, tr)
	if code != exitOK {
		return code
	}

	// A tool that is started beside the translator needs no file. For the
	// others a missing or unreadable setup is left to "ulgen setup", which
	// writes it or says why it cannot.
	proxied := false
	if p, ok := tool.(setup.Proxied); ok {
		proxied = p.Proxied()
	}
	if !proxied && tool.Detect(ctx, env).Installed {
		if configured, err := tool.Configured(env); err != nil || !configured {
			fmt.Fprintln(stdout, tr.T("guide.setting_up", tool.Name()))
			if code := runSetup(ctx, append([]string{tool.ID()}, baseURL...), stdout, stderr, getenv, tr); code != exitOK {
				return code
			}
			fmt.Fprintln(stdout)
		}
	}

	if readGuideTool(getenv) != tool.ID() {
		// Not being able to remember the choice is no reason not to start.
		if writeGuideTool(getenv, tool.ID()) == nil && toolID == "" {
			fmt.Fprintln(stdout, tr.T("guide.remembered", tool.Name()))
		}
	}
	return runRun(ctx, append([]string{tool.ID()}, toolArgs...), stdout, stderr, getenv, tr)
}

// guideSignedIn reports whether there is a key, without printing anything
// when there is none: that is the guide's cue to sign in, not a failure.
func guideSignedIn(stderr io.Writer, getenv func(string) string, tr *i18n.Translator) (bool, int) {
	store, err := openKeyStore(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "ulgen: "+describeStoreError(err, tr))
		return false, exitError
	}
	_, err = credential.Resolve(store, evrenAccount, provider.EVREN.KeyEnv, getenv)
	if errors.Is(err, credential.ErrNotFound) {
		return false, exitOK
	}
	if err != nil {
		fmt.Fprintln(stderr, "ulgen: "+describeStoreError(err, tr))
		return false, exitError
	}
	return true, exitOK
}

// guideTool picks the tool to start: the one named on the command line, else
// the one started last, else the only installed one, else the one the user
// picks from the installed ones.
func guideTool(ctx context.Context, toolID string, env setup.Env, in *bufio.Reader, stdout, stderr io.Writer, getenv func(string) string, tr *i18n.Translator) (setup.Tool, int) {
	if toolID == "" {
		toolID = readGuideTool(getenv)
	}
	if tool, ok := setup.Lookup(toolID); ok {
		return tool, exitOK
	}
	var installed []setup.Tool
	for _, tool := range setup.Tools() {
		if tool.Detect(ctx, env).Installed {
			installed = append(installed, tool)
		}
	}
	switch len(installed) {
	case 0:
		fmt.Fprintln(stderr, "ulgen: "+tr.T("guide.none_installed"))
		for _, tool := range setup.Tools() {
			fmt.Fprintf(stderr, "  %-12s %s\n", tool.Name(), strings.TrimSpace(renderNote(tr, env, tool.InstallHint(env))))
		}
		return nil, exitError
	case 1:
		return installed[0], exitOK
	}
	fmt.Fprintln(stdout, tr.T("guide.choose"))
	for i, tool := range installed {
		fmt.Fprintf(stdout, "  %d) %s\n", i+1, tool.Name())
	}
	for {
		fmt.Fprint(stdout, tr.T("guide.choose_prompt"))
		line, err := interruptible(ctx, func() (string, error) { return readLine(in) }, nil)
		if ctx.Err() != nil {
			fmt.Fprintln(stdout)
			return nil, exitInterrupted
		}
		answer := strings.TrimSpace(line)
		if err != nil && answer == "" {
			// The input ended at the question.
			fmt.Fprintln(stdout)
			fmt.Fprintln(stderr, "ulgen: "+tr.T("guide.no_answer"))
			return nil, exitError
		}
		if answer == "" {
			return installed[0], exitOK
		}
		if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(installed) {
			return installed[n-1], exitOK
		}
		for _, tool := range installed {
			if strings.EqualFold(answer, tool.ID()) {
				return tool, exitOK
			}
		}
		fmt.Fprintln(stdout, tr.T("guide.choose_invalid", len(installed)))
	}
}

// readGuideTool returns the id of the tool the guide started last, or ""
// when there is none or the file cannot be read.
func readGuideTool(getenv func(string) string) string {
	dir, err := configDir(getenv)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, guideFile))
	if err != nil {
		return ""
	}
	var state guideState
	if json.Unmarshal(b, &state) != nil {
		return ""
	}
	return state.Tool
}

// forgetGuideTool drops the remembered tool; the next "ulgen" picks again.
func forgetGuideTool(getenv func(string) string) {
	if dir, err := configDir(getenv); err == nil {
		_ = os.Remove(filepath.Join(dir, guideFile))
	}
}

// writeGuideTool remembers the tool for the next "ulgen".
func writeGuideTool(getenv func(string) string, id string) error {
	dir, err := configDir(getenv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(guideState{Tool: id})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, guideFile), append(b, '\n'), 0o600)
}
