"""Writes src/data/{en,tr}.ts from the captures and the message catalogs.

    python3 make-data.py CAPTURE_DIR

CAPTURE_DIR is the OUT_DIR of capture-cli.py and capture-claude.py (both languages, `ask`).

Every line of the plain commands is copied as captured. The login lines are the catalog's
messages with the values login.go fills in. The Claude Code strings are written here by hand from
the captured screens (CLAUDE below; after a new capture, write in what the new screens show), and
each one is checked against them: the script stops if one does not appear in a snapshot exactly.
The terminal of the video is the size of the capture, 108 x 20.
"""
import json, os, re, sys

capture = sys.argv[1]
repo = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", ".."))
DIM, CODE = "#999999", "#b1b9f9"


def catalog(lang):
    return json.load(open(f"{repo}/internal/i18n/locales/{lang}.json"))


def fmt(message, *args):
    """Go's fmt verbs as the catalogs use them: %s, %d and %[n]s / %[n]d."""
    out, auto = message, iter(args)
    out = re.sub(r"%\[(\d+)\][sd]", lambda m: str(args[int(m.group(1)) - 1]), out)
    out = re.sub(r"%[sd]", lambda m: str(next(auto)), out)
    return out.replace("%%", "%")


def lines(text):
    out = text.replace("\r\n", "\n").split("\n")
    while out and out[-1] == "":
        out.pop()
    return out


CLAUDE = {
    "en": {
        "question": "What does main.go print? One sentence.",
        "preface": "I'll read the file.",
        "summary": [["Thought for ", False], ["4s", True], [", read ", False], ["1", True], [" file", False]],
        "working": [
            ["Bootstrapping…", ""],
            ["Thundering…", "(9s · ↓ 5 tokens)"],
            ["Thundering…", "(11s · ↓ 32 tokens)"],
            ["Thundering…", "(13s · ↓ 54 tokens)"],
            ["Thundering…", "(15s · ↓ 54 tokens · thinking)"],
            ["Thundering…", "(16s · ↓ 59 tokens · thought for 2s)"],
        ],
        "answer": [["It prints ", None], ["6765", CODE], [" (the 20th Fibonacci number, computed recursively).", None]],
        "done": "Cooked for 18s · done 6:42 PM",
    },
    "tr": {
        "question": "main.go ne yazdırır? Tek cümleyle.",
        "preface": None,
        "summary": [["Thought for ", False], ["13s", True], [", read ", False], ["1", True], [" file", False]],
        "working": [
            ["Calculating…", ""],
            ["Creating…", "(6s · thinking)"],
            ["Creating…", "(7s · ↓ 17 tokens · thinking)"],
            ["Creating…", "(8s · ↓ 89 tokens · thought for 1s)"],
            ["Creating…", "(11s · ↓ 98 tokens)"],
            ["Creating…", "(13s · ↓ 100 tokens · thinking)"],
        ],
        "answer": [["main.go", CODE], [", 20. Fibonacci sayısını (6765) yazdırır.", None]],
        "done": "Churned for 14s · done 18:45",
    },
}
COMMON = {
    "header": {"title": "Claude Code", "version": "v2.1.309", "model": "deepseek-v4.1-flash · API Usage Billing", "cwd": "/tmp/ulgen-demo/hello"},
    "effort": "● high · /effort",
    "reading": ("Reading main.go", "⎿  main.go"),
    "footer": {"mode": "⏵⏵ auto mode on", "idle": " (shift+tab to cycle) · ← for agents", "busy": " (shift+tab to cycle) · esc to interrupt", "after": " (shift+tab to cycle)"},
}


def check_on_screen(lang, snippets):
    snaps = json.load(open(f"{capture}/claude-{lang}-ask.json"))
    screens = ["\n".join(s["text"]) for s in snaps]
    for snippet in snippets:
        if not any(snippet in screen for screen in screens):
            sys.exit(f"{lang}: not on any captured screen: {snippet!r}")
    return screens


def ts(value):
    return json.dumps(value, ensure_ascii=False)


def spans(items):
    return "[" + ", ".join(ts({k: v for k, v in item.items() if v is not None and v is not False}) for item in items) + "]"


for lang in ("en", "tr"):
    cat = catalog(lang)
    cli = json.load(open(f"{capture}/{lang}.json"))
    c = CLAUDE[lang]
    raw = open(f"{capture}/claude-{lang}-ask.raw", "rb").read().decode("utf-8", "replace")
    notice = "ulgen: " + cat["run.claude_notice"]
    assert notice in raw, f"{lang}: the notice is not in the capture"
    summary = "".join(t for t, _ in c["summary"])
    answer = "".join(t for t, _ in c["answer"])
    check_on_screen(lang, [
        COMMON["header"]["title"], COMMON["header"]["version"], COMMON["header"]["model"], COMMON["header"]["cwd"], COMMON["effort"],
        "❯ " + c["question"], summary, "● " + answer, c["done"], *COMMON["reading"],
        *(COMMON["footer"]["mode"] + COMMON["footer"][k] for k in ("idle", "busy", "after")),
        *((w + " " + s) if s else w for w, s in c["working"]),
        *(["● " + c["preface"]] if c["preface"] else []),
    ])
    login = {
        "openPage": lines(fmt(cat["login.open_page"], "https://evren.ssyz.org.tr/api-keys")),
        "keyPrompt": cat["login.key_prompt"],
        "termsIntro": fmt(cat["login.terms_intro"], 1),
        "acceptPrompt": cat["login.accept_prompt"],
        "yes": {"en": "y", "tr": "e"}[lang],
        # login is not run for the video (it would need a key pasted); the time shown is a minute
        # before the setup's backup (20261023-183946).
        "accepted": fmt(cat["login.accepted"], 1, "2026-10-23 18:38"),
        "saved": cat["login.saved_keyring"],
    }
    claude = f"""{{
    notice: {ts(notice)},
    header: {ts(COMMON["header"])},
    effort: {ts(COMMON["effort"])},
    question: {ts(c["question"])},
{f"    preface: {ts(c['preface'])}," + chr(10) if c["preface"] else ""}    reading: {{
      title: {ts(COMMON["reading"][0])},
      file: "main.go",
      summary: {spans({"text": t, "color": DIM, "bold": b} for t, b in c["summary"])},
    }},
    working: [
{"".join(f"      {{ word: {ts(w)}, status: {ts(s)} }},{chr(10)}" for w, s in c["working"])}    ],
    answer: {spans({"text": t, "color": col} for t, col in c["answer"])},
    done: {ts(c["done"])},
    footer: {ts(COMMON["footer"])},
  }}"""

    def block(name):
        return "[\n" + "".join(f"    {ts(line)},\n" for line in lines(cli[name]["text"])) + "  ]"

    title = {"en": "English", "tr": "Turkish"}[lang]
    body = f"""// The terminal text of the {title} video. Generated from a real run of ulgen 0.2.0 on 2026-10-23
// (LANG={'en_US' if lang == 'en' else 'tr_TR'}.UTF-8, in a sandboxed HOME); see "Where the text comes from" in README.md.
// Paths are shown as ulgen prints them (~/…); nothing here names a user or a machine.
import type {{ DemoData }} from "./types";

export const data: DemoData = {{
  login: {ts(login)},
  setupList: {block("setup-list")},
  setupDryRun: {block("setup-dry-run")},
  setupApply: {block("setup-apply")},
  doctor: {block("doctor")},
  status: {block("status")},
  claude: {claude},
}};
"""
    path = f"{repo}/video/src/data/{lang}.ts"
    open(path, "w").write(body)
    print("wrote", path)
