---
name: evren-workers
description: Hand a fully specified implementation task to an EVREN worker, a headless Claude Code process on a free EVREN model, started with "ulgen worker". Use it when a plan has routine tasks to hand out, or when the user asks for an EVREN worker.
---
<!-- installed by ulgen setup; "ulgen setup claude" rewrites this file -->

# EVREN workers

An EVREN worker is one headless Claude Code process that does one exactly
specified task on an EVREN model, unattended, through Ülgen's translator.
You write the task, start the worker, and check the result yourself. This
skill tells you when to do that, how to write the task, how to start it, how
to check it, and what it costs.

## When to delegate

Hand a task to a worker when every decision in it is already made: the files,
the behaviour, the names, and the tests that must pass.

Do not hand over design, open-ended review, security-sensitive code, or
anything where the worker would have to choose. If you cannot write down the
exact final commands that must pass, the task is not ready.

A second failure on the same piece means it comes back to you: stop starting
workers for it and do it yourself.

## How to write the task file

Write one file per task. The worker knows nothing of your conversation, so
the file must be self-contained: what the project is in two sentences; what
to read first; the decisions, each stated as a decision; the files it may
touch; the tests that must fail before and pass after; and the exact final
commands.

Use this shape, and copy its last section into every task file word for
word:

```
You are implementing <one part> of <project>: <what the project is, in two
sentences>. Your working directory is the repository root.

Read first: <the files, in order, and what each is for>.

## What to build
<Each decision stated as a decision: the files to create or change, the
names, the behaviour, the error cases. Nothing left to choose.>

## Tests
<The tests to write first, by name or by case; each must fail before the
change and pass after it.>

## Final checks, all must pass
<The exact commands: formatter, build, tests with a timeout.>

## Standing rules
- You work unattended. Nobody reads your messages while you work, and nobody
  can answer a question or approve a plan. Never stop to present a plan; do
  the task to the end.
- A refused command is not on your allow-list. Run commands one at a time,
  without pipes or redirection, or use your file tools, and carry on.
- Create and edit files with your file tools, not with shell redirection.
- Change only the files this task names.
- Never weaken, skip or delete a test to make it pass.
- Run every test with a timeout, so that a loop cannot eat your time.
- Do not commit, push, or run any git command that changes history.
- As soon as the checks pass, write REPORT.md in the repository root: what
  you changed, the tests by name, what you could not do, and the last lines
  of the final commands. Do not leave the report for the last minute.
- Reply in at most ten lines.
```

A task file that leaves the worker to guess is a task you have not finished
specifying. When you notice yourself writing "as appropriate" or "handle the
edge cases", stop and make the decision.

## How to start it

Start the worker as a background command, in a separate clone or worktree of
the repository, so that it never edits the tree you are working in:

```sh
ulgen worker --dir /path/to/clone --allow go --timeout 60m --out /path/to/result.json /path/to/task.md
```

`--dir` is the project the worker edits; it must not be your home directory,
a directory above it, or a file system root. What a worker can do there, and
only there:

- Its file tools edit and create files under `--dir` and nowhere else, and
  read files there. A rule such as `--allow "Read(~/refs/**)"` lets them read
  another place; the usual places of keys and credentials stay closed to them
  whatever you allow.
- Its shell runs only the commands on the allow-list. By default that is
  `git status`, `git diff`, `git log`, `ls` and `mkdir`. `--allow` adds a
  preset (`go`, `node`, `python`, `read`) or one permission rule.
- Claude Code's sandbox lets those commands write only under `--dir` and the
  temporary directory, and keeps them from reading the usual places of keys
  and credentials. It does not keep them from reading other files on the
  machine: a preset such as `go` or `python` runs code, and that code can
  read what you can read.
- With `--no-sandbox`, and on Windows (where Claude Code has no sandbox and
  `--no-sandbox` is required), the shell commands are not confined at all.
  Do not use it for a task or a repository you would not run by hand.

`--timeout` ends the worker and everything it started after that long (the
default is 30 minutes; `0` means none); the exit code is then 124. `--out`
writes the worker's whole JSON answer to a file. Several independent tasks
can run at once, each in its own clone.

Give the worker no rule that lets it commit or push. `ulgen worker` refuses a
rule for a shell or a command runner, a rule for an editing tool, and a value
that holds more than one rule. When a task needs more than a preset gives,
add the one rule it needs, such as `--allow "Bash(make test:*)"`. A task that
needs a wide rule is a task to do yourself.

Start the command in the background and keep working while it runs, or wait
for it if the next step needs its result. Keep the task file and the result
file: together with the clone and the diff, they are the whole record of what
the worker was asked and what it did.

## How to check the result

Never trust the worker's summary. Run the formatter, the build and the tests
yourself, with a timeout. Read the diff, all of it for code that handles
secrets or users' files. Then commit yourself. What the worker reports as
"could not do" is the first thing to read.

A line on the worker's standard error that begins with
`[claude-code:unrecognized_model]` is Claude Code saying that it does not know
EVREN's model names. It is expected and says nothing about the result.

A worker that ran out of time may still have finished the code and missed
only its report: run your checks before you decide. When the checks fail,
throw the clone away rather than build on it, and start a fresh one from a
task you have made clearer.

## What it costs

A worker spends EVREN's daily quota (`ulgen status`), not Claude usage. A
worker is slower than a subagent and cannot ask questions. Anthropic does not
support running Claude Code this way; the notice of `ulgen run claude`
applies to workers too.
