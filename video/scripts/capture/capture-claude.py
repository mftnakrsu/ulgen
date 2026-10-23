"""Records an interactive `ulgen claude` session in a pseudo-terminal, with pyte as the screen.

    python3 capture-claude.py ULGEN_DIR OUT_DIR LANG check
    python3 capture-claude.py ULGEN_DIR OUT_DIR LANG ask "What does main.go print? One sentence."

ULGEN_DIR holds the ulgen binary (it is put first on PATH); Claude Code must be installed. Needs
pyte (pip install pyte). Snapshots of the screen (the text and the colours of every cell) go to
OUT_DIR/claude-<lang>-<mode>.json, the raw bytes next to them; keep OUT_DIR outside the repository.

The setting is the one of docs/assets/demo.tape: the project /tmp/ulgen-demo/hello with the small
main.go, Claude Code's own configuration in CLAUDE_CONFIG_DIR=/tmp/ulgen-demo/cfg (onboarding
done, dark theme, the project trusted), PS1='$ ', a clean environment and HOME in the sandbox of
capture-cli.py. Both are made here when they are missing.

`check` starts ulgen claude and quits without sending a prompt, so it costs no EVREN tokens. Run it
once on a new configuration: Claude Code shows its one-time notices then, as the hidden part of
demo.tape does. `ask` types the question and waits for the answer to name 6765; it costs EVREN
quota (a few thousand tokens).
"""
import fcntl, json, os, pty, select, struct, sys, termios, time
import pyte

ulgen_dir, out_dir, lang, mode = sys.argv[1:5]
question = sys.argv[5] if len(sys.argv) > 5 else ""
assert lang in ("en", "tr") and mode in ("check", "ask") and (question or mode == "check")
COLS, ROWS = 108, 20  # the terminal of the video (src/scenes/layout.ts)
home = f"/tmp/ulgen-demo/home-{lang}"
project, config = "/tmp/ulgen-demo/hello", "/tmp/ulgen-demo/cfg"
os.makedirs(home, exist_ok=True)
os.makedirs(project, exist_ok=True)
os.makedirs(config, exist_ok=True)
if not os.path.exists(f"{project}/main.go"):
    with open(f"{project}/main.go", "w") as f:
        f.write('package main\n\nimport "fmt"\n\n// fib returns the n-th Fibonacci number.\nfunc fib(n int) int {\n'
                "\tif n < 2 {\n\t\treturn n\n\t}\n\treturn fib(n-1) + fib(n-2)\n}\n\nfunc main() {\n\tfmt.Println(fib(20))\n}\n")
if not os.path.exists(f"{config}/.claude.json"):
    with open(f"{config}/.claude.json", "w") as f:
        json.dump({"hasCompletedOnboarding": True, "theme": "dark", "projects": {project: {"hasTrustDialogAccepted": True}}}, f, indent=2)

env = {
    "HOME": home,
    "XDG_CONFIG_HOME": f"{home}/.config",
    "PATH": f"{ulgen_dir}:{os.environ['PATH']}",
    "LANG": {"en": "en_US.UTF-8", "tr": "tr_TR.UTF-8"}[lang],
    "TERM": "xterm-256color",
    "COLORTERM": "truecolor",
    "DBUS_SESSION_BUS_ADDRESS": os.environ["DBUS_SESSION_BUS_ADDRESS"],
    "XDG_RUNTIME_DIR": os.environ["XDG_RUNTIME_DIR"],
    "USER": "demo",
    "LOGNAME": "demo",
    "SHELL": "/bin/bash",
    "PS1": "$ ",
    "CLAUDE_CONFIG_DIR": config,
}

pid, fd = pty.fork()
if pid == 0:
    os.chdir(project)
    os.execve("/bin/bash", ["bash", "--noprofile", "--norc", "-i"], env)
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))


class Screen(pyte.Screen):
    def write_process_input(self, data):  # answers the terminal's status queries
        os.write(fd, data.encode())


screen = Screen(COLS, ROWS)
stream = pyte.ByteStream(screen)
os.makedirs(out_dir, exist_ok=True)
raw = open(f"{out_dir}/claude-{lang}-{mode}.raw", "wb")
snapshots = []
t0 = time.time()


def pump(seconds):
    end = time.time() + seconds
    while (left := end - time.time()) > 0:
        ready, _, _ = select.select([fd], [], [], left)
        if ready:
            try:
                data = os.read(fd, 65536)
            except OSError:
                return
            raw.write(data)
            stream.feed(data)


def snap(label):
    cells = [[[c.data, c.fg, c.bg, c.bold, c.italics, c.reverse] for c in (screen.buffer[y][x] for x in range(COLS))] for y in range(ROWS)]
    snapshots.append({"t": round(time.time() - t0, 2), "label": label, "text": list(screen.display), "cursor": [screen.cursor.x, screen.cursor.y], "cells": cells})
    print(f"--- {label} t={snapshots[-1]['t']}")
    print("\n".join(line.rstrip() for line in screen.display))


def typed(text, delay=0.06):
    for char in text:
        os.write(fd, char.encode())
        pump(delay)


pump(1.5)
typed("ulgen claude")
os.write(fd, b"\r")
for i in range(16):
    pump(0.5)
    snap(f"start {i}")
if mode == "ask":
    typed(question, 0.07)
    snap("typed")
    pump(0.4)
    os.write(fd, b"\r")
    deadline, answered_at, i = time.time() + 120, None, 0
    while time.time() < deadline:
        pump(0.5)
        i += 1
        snap(f"wait {i}")
        if answered_at is None and "6765" in "\n".join(screen.display):
            answered_at = time.time()
        if answered_at and time.time() - answered_at > 6:
            break
typed("/exit", 0.08)
pump(0.8)
os.write(fd, b"\r")
pump(1.5)
if "/exit" in "\n".join(screen.display):  # Enter first picked the suggestion
    os.write(fd, b"\r")
pump(3)
snap("after exit")
typed("exit")
os.write(fd, b"\r")
pump(1)
raw.close()
json.dump(snapshots, open(f"{out_dir}/claude-{lang}-{mode}.json", "w"), ensure_ascii=False)
