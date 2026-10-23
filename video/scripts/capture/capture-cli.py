"""Runs ulgen's plain commands in a pseudo-terminal, in a sandboxed HOME, and keeps their output.

    python3 capture-cli.py ULGEN OUT_DIR LANG        (LANG: en or tr)

Writes OUT_DIR/<lang>.json (the text of each command) and the raw bytes next to it. Keep OUT_DIR
outside the repository; only make-data.py's output is committed.

What it runs: `setup` (the list), `setup opencode --dry-run`, `setup opencode`, `doctor` and
`status`, with HOME and XDG_CONFIG_HOME in /tmp/ulgen-demo/home-<lang>, which is made afresh with
an opencode.json of the user's own (so setup shows an update and its backup). The key comes from
the system keychain over D-Bus and is never printed by these commands. The script never runs
login, logout or token, and never writes outside /tmp/ulgen-demo. doctor and status ask EVREN for
the quota and the models, which costs no tokens.
"""
import fcntl, json, os, pty, select, shutil, struct, sys, termios

ulgen, out_dir, lang = sys.argv[1], sys.argv[2], sys.argv[3]
COLS, ROWS = 108, 20  # the terminal of the video (src/scenes/layout.ts)
home = f"/tmp/ulgen-demo/home-{lang}"
assert lang in ("en", "tr") and home.startswith("/tmp/ulgen-demo/")
if os.path.exists(home):
    shutil.rmtree(home)
os.makedirs(f"{home}/.config/opencode")
with open(f"{home}/.config/opencode/opencode.json", "w") as f:
    f.write('{\n  "$schema": "https://opencode.ai/config.json",\n  "theme": "opencode"\n}\n')

# A clean environment: nothing of the shell (or of a Claude Code session) this runs from.
env = {
    "HOME": home,
    "XDG_CONFIG_HOME": f"{home}/.config",
    "PATH": os.environ["PATH"],
    "LANG": {"en": "en_US.UTF-8", "tr": "tr_TR.UTF-8"}[lang],
    "TERM": "xterm-256color",
    "COLORTERM": "truecolor",
    "DBUS_SESSION_BUS_ADDRESS": os.environ["DBUS_SESSION_BUS_ADDRESS"],
    "XDG_RUNTIME_DIR": os.environ["XDG_RUNTIME_DIR"],
    "USER": "demo",
    "LOGNAME": "demo",
}


def run(args):
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(home)
        os.execve(ulgen, [ulgen, *args], env)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    chunks = []
    while True:
        ready, _, _ = select.select([fd], [], [], 60)
        if not ready:
            break
        try:
            data = os.read(fd, 65536)
        except OSError:
            break
        if not data:
            break
        chunks.append(data)
    _, status = os.waitpid(pid, 0)
    return b"".join(chunks), os.waitstatus_to_exitcode(status)


runs = {
    "setup-list": ["setup"],
    "setup-dry-run": ["setup", "opencode", "--dry-run"],
    "setup-apply": ["setup", "opencode"],
    "doctor": ["doctor"],
    "status": ["status"],
}
os.makedirs(out_dir, exist_ok=True)
result = {}
for name, args in runs.items():
    raw, code = run(args)
    open(f"{out_dir}/{lang}-{name}.raw", "wb").write(raw)
    text = raw.decode("utf-8", "replace").replace("\r\n", "\n")
    # ulgen prints no colours; an escape sequence here would need handling in the video.
    result[name] = {"args": args, "exit": code, "escapes": "\x1b" in text, "text": text}
    print(f"{lang} {name}: exit {code}, escape sequences: {result[name]['escapes']}")
json.dump(result, open(f"{out_dir}/{lang}.json", "w"), ensure_ascii=False, indent=1)
