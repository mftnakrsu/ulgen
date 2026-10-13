# evren-workers

A Claude Code plugin that carries one skill, `evren-workers`. The skill
teaches an orchestrating Claude Code session when to hand a fully specified
implementation task to an EVREN worker — one headless Claude Code process on
a free EVREN model, started with `ulgen worker` — how to write the task file,
how to start the worker, how to check its result, and what it costs.

`ulgen setup claude` installs the skill into the user's Claude directory
(`$CLAUDE_CONFIG_DIR/skills/evren-workers/SKILL.md`, else
`~/.claude/skills/evren-workers/SKILL.md`); the file it writes carries a
marker comment and is rewritten on every `ulgen setup claude` that changes
it.

This directory is also a complete Claude Code plugin: `.claude-plugin/plugin.json`
describes it, and the skill text is `skills/evren-workers/SKILL.md`. Nothing
is published yet; the directory is in the repository so that it can serve as
a marketplace one day.

The Go file `skill.go` embeds the same `SKILL.md` into the Ülgen binary.

The plugin is licensed AGPL-3.0-only, the license of the Ülgen repository
(see the repository's `LICENSE`).
