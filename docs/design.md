# How Ülgen is put together

Ülgen connects EVREN to coding tools that already exist. It signs in, keeps
the key, writes each tool's settings and starts the tool. It is not a coding
agent and does not replace the tools.

This document records the decisions someone reading the code would ask about.

## Layout

| Path | What it holds |
|---|---|
| `cmd/ulgen` | The command line: one file per subcommand, the translation of errors into text, starting tools on each system |
| `internal/credential` | Where the key is kept: the system's credential store, or a file only the user can read |
| `internal/evren` | EVREN's own endpoints: terms, quota, model details |
| `internal/provider` | The HTTP client under `internal/evren`: bounded retries, no redirects, the key removed from errors |
| `internal/setup` | One adapter per tool (Qwen Code, opencode, Codex CLI, Claude Code): what to write, the checks, the writer |
| `internal/setup/jsonc` | An editor for JSON with comments that changes one value and leaves the rest of the file byte for byte |
| `internal/proxy` | The translator between the Anthropic Messages API and EVREN's chat completions |
| `internal/i18n` | Turkish and English messages; both ship in the binary |
| `internal/diff`, `internal/termsafe`, `internal/version` | Unified diffs for the dry run; cleaning text before it reaches a terminal; the version |
| `plugins/claude-code/evren-workers` | The skill that tells Claude Code how to hand a task to an EVREN worker |

`internal/setup` returns typed errors and notes as message keys;
`cmd/ulgen` turns them into text. The core does not know the interface
language.

## The guide

`ulgen` alone, on a terminal, takes someone from nothing to a running tool:
it signs in when there is no key, picks the tool (the one started last, else
the only installed one, else it asks), sets it up when it is not, and starts
it. `ulgen <tool>` does the same for a named tool. The guide calls `login`,
`setup` and `run` with the arguments a user would type and has no checks of
its own, so none of theirs can be skipped. Without a terminal `ulgen` prints
the usage.

## The key

- **Stored only after it works.** `ulgen login` stores the key after EVREN
  accepted it and the terms were accepted. A cancelled re-login never loses
  the key that was there.
- **Where.** The macOS Keychain, Windows Credential Manager or the Linux
  Secret Service. Where there is none, a file with mode 0600 in a 0700
  directory; `ULGEN_NO_KEYRING=1` chooses the file. `EVREN_API_KEY` in the
  environment takes precedence over both.
- **Never in a tool's file.** Every file Ülgen writes names the key only by
  its variable, `EVREN_API_KEY`. The tools read the value from their
  environment, put there by `ulgen run`.
- **Never printed.** No command prints, logs or puts the key into an error
  message. A key that a server repeats in an error body is replaced by
  `[key]` before anything is cut or parsed. The one exception is the standard
  output of `ulgen token`, which refuses to write to a terminal without
  `--show`.
- **Never sent in clear text.** `--base-url` must be https, or http to this
  machine only, and a key that goes to another host than EVREN's is announced.

## Writing a tool's settings

The files belong to the user and to the tool. `ulgen setup` changes as little
as it can and refuses when it is not sure.

- **The edit is committed only if the result is safe.** A change is applied
  when the result parses, means exactly what was asked and keeps every
  comment; otherwise nothing is written.
- **Every check runs before any write.** A refusal at the second file leaves
  the first untouched. `--dry-run` runs the same checks, so it reports the
  refusals a real run would hit.
- **Every changed file is backed up first**, one directory per run under
  Ülgen's configuration directory. A backup is never written over.
- **Nothing is overwritten by accident.** A new file is put in place with a
  hard link, which fails if the file appeared meanwhile; an existing file is
  re-read and compared right before the rename. Symbolic links are followed,
  read-only files are left alone.
- **An entry is Ülgen's by its key reference, not by its name.** An entry that
  reads the key from `EVREN_API_KEY` is Ülgen's and may be rewritten. A user's
  own entry under EVREN's name is refused, not overwritten.
- **Ülgen adds a default, it does not take one over.** The default model is
  written when the tool has none or `--default` asks; a choice the user made
  stays, with a note.
- **`--remove` is the same machinery backwards.** It takes out what is
  Ülgen's by the key-reference rule (its entries, a default that pointed at
  one of them, the files it installed) with the same checks and
  backups, needs neither the key nor the network, and leaves what the user
  wrote. One thing it cannot tell from the user's own choice stays: Qwen
  Code's usage-statistics setting.
- **Codex's `config.toml` is never written.** Ülgen writes
  `evren.config.toml` beside it, which Codex lays over its own file with
  `--profile evren`.
- **A dry run hides secrets.** The diff is stripped of control characters, the
  key is replaced by `[key]` and values whose names mark them as secrets by
  `[hidden]`. A diff is what people paste into a bug report.
- **No text of the user's file goes into an error message.**
- **Paths from the environment must be absolute.** A relative `QWEN_HOME`
  would mean the working directory, where a cloned repository can plant files.
- **Qwen Code's usage statistics are turned off** when the setting is absent;
  a value that is there stays.

## The project check

Qwen Code and opencode read settings from the project directory, and a
project's file wins over the user's. A cloned repository can therefore carry a
file that sends the key to any address. `ulgen run` walks up from the working
directory and refuses to start the tool when such a file names the key or the
EVREN provider, or cannot be read. `--trust-project` starts it anyway.

Codex needs no check (its project files cannot define providers), and neither
does Claude Code (it never holds the key, see below).

## The translator

Claude Code speaks the Anthropic Messages API; EVREN speaks chat completions.
`ulgen run claude` starts a translator inside Ülgen for the length of that
run.

- **Inside Ülgen, not a separate router.** One binary, no other runtime, no
  server that outlives the tool, and the key stays in Ülgen's memory.
- **Claude Code never gets the key.** It is given a random token that only the
  local translator accepts and that is good for this run. `EVREN_API_KEY` and
  Anthropic's own credential variables are removed from its environment.
- **Loopback only.** The translator listens on `127.0.0.1` on a port the
  system picks.
- **Unknown fields are dropped, not passed on.** Reasoning a model sends is
  shown as a thinking block.
- **Claude Code's effort reaches the model.** For a model that EVREN lists as
  taking a `reasoning_effort`, the effort Claude Code sends (`/effort`,
  `--effort`) is passed on: low, medium, high and xhigh as they are, max as
  xhigh, because some EVREN models refuse max and all of them take xhigh.
  With thinking off, which Claude Code shows by leaving `thinking` out, the
  model is asked for `none`. A value outside the model's own list is not
  sent, and a model that takes no effort gets none: EVREN refuses the field
  for it.
- **A system message between the turns travels in a user turn.** To a model
  it does not know, Claude Code sends the working directory, the platform and
  the date in a message of role `system` after the first user turn, and a
  short one after every later turn. Left out, the model has to guess where it
  is, and it searches the disk for the project. EVREN's models do not all take
  a system message in that place (one refuses the request, one ignores the
  message), so its text is put into the user turn next to it, inside the
  `<system-reminder>` tags Claude Code itself uses for such notes.
- **The provider is not trusted further than it must be.** A redirect is not
  followed, so the key and the prompt go only to the configured address, and
  one event of a streamed answer, like a whole answer that is not streamed,
  may be 64 MB at most.
- **Token counts are estimates**, UTF-8 bytes divided by 3 and rounded up,
  erring high so Claude Code compacts early rather than late.
- **A refused key is reported as a plain error that names `ulgen login`**,
  because Claude Code answers a 401 with advice to sign in to Anthropic.
- **The notice.** Anthropic does not support running Claude Code on other
  models. `ulgen run claude` says so once per run; `ULGEN_QUIET_NOTICE=1`
  leaves the line out.
- **Claude Code is told the model's context window.** For a model it does not
  know, Claude Code assumes 200k tokens and warns about it at every start.
  `CLAUDE_CODE_MAX_CONTEXT_TOKENS`, set to the window EVREN reports, does away
  with both; a value the caller set stays.
- **Claude Code's unrecognized-model line is left as it is.** In a `-p` run
  Claude Code writes `[claude-code:unrecognized_model] {…}` to standard error
  once for each model it does not know. Both ways to stop it cost more than
  the line. A `modelOverrides` entry makes Claude Code take the EVREN model for
  a Claude model, with that model's name, window and features. Filtering the
  line means taking Claude Code's standard error off the terminal, and Claude
  Code then leaves out the warnings it writes only to a terminal.
- **Only documented variables of Claude Code are used.**

`ulgen proxy` runs the same translator on its own for another program that
speaks the Anthropic API.

## Workers

`ulgen worker` runs one headless Claude Code process on an EVREN model to do a
task written in a file, unattended. Nobody is there to answer a permission
prompt, so the fence is built before the process starts.

- **A separate process in bare mode**: none of the user's plugins, hooks,
  skills or memory are loaded.
- **`dontAsk` with an allow-list.** Whatever is not allowed is denied.
  Editing under `--dir` and a few read-only commands are always allowed;
  `--allow` adds one rule or a preset (`go`, `node`, `python`, `read`).
- **Catch-all rules are refused.** A rule for a shell or a command runner
  (`sh`, `env`, `xargs`, `sudo`, …) would allow everything.
- **Claude Code's own sandbox, fail-closed.** Shell commands can write only
  under `--dir` and the temporary directory. A system without a sandbox stops
  the worker unless `--no-sandbox` is given.
- **The key places are closed** (`~/.ssh`, credential directories, Ülgen's own
  directory) to shell commands and to the file tools alike, also with
  `--no-sandbox`.
- **`--dir` is resolved and checked**: a symbolic link to the root or to the
  home directory is refused.
- **`--out` is never written through a link.** Ülgen writes that file itself,
  outside the sandbox, and the worker can often write next to it. The file is
  opened before the worker starts and the answer goes to that open file; a
  symbolic link at the path is refused, and a path that names another file at
  the end is reported.
- **No commit or push by default.** A worker changes files; the session that
  started it checks and commits.
- **A timeout ends everything the worker started**, its process group
  included.

`ulgen setup claude` installs a skill that tells Claude Code when and how to
start a worker. A `SKILL.md` without Ülgen's marker is the user's own and is
left alone.

## Starting a tool on Windows

npm installs the tools as `.cmd` files. The file npm writes does one thing:
it starts `node <script> <arguments>`. When a tool's file is that one, to the
letter, Ülgen starts node with the script itself, so `cmd.exe` is not involved
and every argument arrives as it is. Node is the copy beside the file, else
the one on `PATH`; never one from the working directory.

Any other batch file can only be run by `cmd.exe`. Ülgen then builds the
command line itself and refuses an argument that holds `"`, `%`, `!`, `^` or a
control character rather than escape it: the rules `cmd.exe` reads a line by
are not the ones Go quotes for.

## Language

The interface follows the operating system's language, Turkish or English.
Every message lives in `internal/i18n/locales`; a test keeps the two files in
step.

## Licence

Ülgen is AGPL-3.0-only: anyone may use it, and anyone who changes and
distributes it publishes the changes under the same terms. Dependencies carry
compatible licences.
