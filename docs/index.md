# Ülgen

EVREN for the coding tools you already use.

Ülgen connects [EVREN](evren.md), the model API run by Türkiye's Presidency of Defence Industries (SSB), to Qwen Code, opencode, Codex CLI and Claude Code. It signs you in, keeps the key safe, accepts the terms, watches the quota and starts the tool you already use. It is one binary, and it is not a new coding agent.

<p align="center">
  <img src="assets/demo.gif" alt="ulgen doctor, ulgen status, and Claude Code answering a question about a file through EVREN" width="760">
</p>

The recording above is real: `ulgen doctor`, `ulgen status`, then Claude Code reading a file and answering through EVREN. It is made from [demo.tape](https://github.com/mftnakrsu/ulgen/blob/main/docs/assets/demo.tape) with [vhs](https://github.com/charmbracelet/vhs), played back faster than it ran.

## Install

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.ps1 | iex
```

Packages, a build from source and checking a download are on the [install](install.md) page. What the commands do is under [Using Ülgen](using.md). The long guide in Turkish is [EVREN'i kodlama araçlarınızda kullanma](tr/evren-ile-kodlama-araclari.md).
