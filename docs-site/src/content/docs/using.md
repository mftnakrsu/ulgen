---
title: Using Ülgen
description: The first run, what you can do with Ülgen, the supported tools, the commands, how the key is kept and the known limits.
---

## First run

You need an EVREN account (sign-in is through e-Devlet) and one of the supported tools installed.

```sh
ulgen
```

That is all. On the first run Ülgen signs you in, finds the tools you have installed, asks which one you want, sets it up for EVREN and starts it. Later runs start that tool straight away; `ulgen opencode` (or `qwen`, `codex`, `claude`) switches to another one, and anything after the name is passed to the tool.

Each step is also a command of its own:

```sh
ulgen login        # opens EVREN's API keys page, asks for the key, shows the terms, saves the key
ulgen setup qwen   # writes the EVREN provider into Qwen Code
ulgen run qwen     # starts Qwen Code with the key in its environment
```

`ulgen status` shows the remaining daily quota and the models. `ulgen setup` on its own lists the tools and their state. When something does not work, `ulgen doctor` checks the key, EVREN and every tool and says what to do.

## What you can do with it

```sh
ulgen                                   # pick a tool once, then just start coding
ulgen claude                            # Claude Code on an EVREN model, in the current project
ulgen qwen -p "explain this repository" # one question, answered in the terminal
ulgen doctor                            # something is off: what, and what to do about it
ulgen status                            # how much of today's quota is left
ulgen setup --remove all                # take Ülgen's settings out of every tool again
```

## Supported tools

| Tool | `ulgen setup <name>` | What setup writes |
|---|---|---|
| Qwen Code | `qwen` | The EVREN models in `settings.json` |
| opencode | `opencode` | The EVREN provider in `opencode.json` |
| Codex CLI | `codex` | A profile file of its own, `evren.config.toml`; your `config.toml` is never written |
| Claude Code | `claude` | Only a skill for EVREN workers; `ulgen run claude` needs no settings file |

Every changed file is backed up first, and `--dry-run` shows the change without writing it. `ulgen setup --remove <name>` undoes it: what Ülgen wrote goes, what you wrote stays.

## Commands

```
  ulgen            sign in, pick a tool, set it up and start it
  ulgen <tool>     the same for one tool: qwen, opencode, codex, claude

  ulgen login      sign in to EVREN: opens the API keys page, saves the key, records that you accepted the terms
  ulgen logout     remove the saved key
  ulgen status     remaining daily quota and models
  ulgen doctor     check the key, EVREN and the tools; say what to do about what is wrong
  ulgen setup      set up coding tools for EVREN (qwen, opencode, codex, claude)
  ulgen run        start a tool with the key in its environment; Claude Code through the translator

Advanced:
  ulgen worker     run one headless Claude Code on an EVREN model to do a task unattended
  ulgen proxy      run the translator on its own, for a program that speaks the Anthropic API
  ulgen token      print the saved key for another program
  ulgen version    print the version
```

`ulgen <command> -h` lists a command's options. Every command follows the system language, Turkish or English.

A full guide in Turkish: [EVREN'i kodlama araçlarınızda kullanma](/tr/evren-ile-kodlama-araclari/).

## Your key stays yours

- It is stored in the operating system's credential store (or, where the system has none, in a file only you can read), and only after EVREN accepted it.
- It is never written into a tool's settings file; the tools get it from their environment when `ulgen run` starts them.
- It is never printed or logged, and it is removed from error messages a server sends back.
- Claude Code never receives it at all: it talks to a translator inside Ülgen on your own machine.
- `ulgen run` refuses to start a tool in a project whose own settings file could send the key elsewhere.

The reasoning behind these and the other decisions is in [docs/design.md](https://github.com/mftnakrsu/ulgen/blob/main/docs/design.md).

## Known limits

- Anthropic does not support using Claude Code with other models. `ulgen run claude` works through a translator and says so once per run.
- In a one-shot run (`ulgen claude -p`), Claude Code writes one line to standard error, `[claude-code:unrecognized_model] {…}`, because it does not know EVREN's model names. It is a note, not an error: the request goes out unchanged.
- Token counts shown by Claude Code on EVREN are estimates.
- When it ends, Claude Code suggests `claude --resume <id>`. Run as it is, that command does not go through Ülgen; `ulgen claude --resume <id>` resumes the session on EVREN.
- On Windows, a tool whose launcher is not the standard one npm writes is started through `cmd.exe`, and then an argument may not contain `"`, `%`, `^` or `!`.
