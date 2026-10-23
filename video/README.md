# Ülgen video

The product demo and the gallery stills of the website, made with [Remotion](https://www.remotion.dev/).
Ülgen has no window of its own, so the video is a terminal drawn natively: commands are typed, the
output appears line by line as the program prints it, the cursor blinks. Every line of output is
taken from real runs (see [Where the text comes from](#where-the-text-comes-from)); Claude Code's
screen is redrawn from a captured session, with the colours it printed.

## What it makes

| Composition | Output (in `out/`) |
| --- | --- |
| `UlgenDemo-en`, `UlgenDemo-tr` | `ulgen-demo-<lang>.mp4`: 40.5 s, 1920×1080, 30 fps, H.264, no audio |
| `UlgenDemo-<lang>` with `captions: true` | `ulgen-demo-<lang>-captioned.mp4`: the same with the subtitles burned in |
| `UlgenDemo-<lang>` with subtitles, voiceover and music | `ulgen-demo-<lang>-voiced.mp4`: AAC audio (`npm run render:voiced`) |
| (stills of the demo, frame 930: Claude Code's answer) | `ulgen-demo-<lang>-poster.png` |
| `still-doctor`, `still-status`, `still-setup`, `still-claude` | `ulgen-<name>.png`: 2160×1350 stills for the gallery of the website's product page (English) |
| (from `src/captions.ts`) | `captions/ulgen-demo-<lang>.srt`, `.vtt`, `.json` |

`<lang>` is `en` or `tr`. The scenes (folder _Scenes_ in the Studio) are also registered on their
own for previewing, and the stills in _Stills_.

The demo, scene by scene (each fades into the next over 0.5 s):

| Scene | Length | What it shows |
| --- | --- | --- |
| Opening | 3.5 s | The logo draws itself; "Ülgen" and the README's tagline |
| Login | 6.5 s | `ulgen login`: the API keys page, the key pasted hidden, the terms accepted, the key saved in the system keychain |
| Setup | 7 s | `ulgen setup opencode --dry-run` (the diff scrolls by, nothing is written), then `ulgen setup opencode` with its backup |
| Health | 7.5 s | `ulgen doctor`, then `ulgen status` (quota and models) |
| Claude Code | 10 s | `ulgen claude` in a small project; Claude Code on an EVREN model reads `main.go` and answers 6765 |
| Key | 4.5 s | Three lines of the README's "Your key stays yours" |
| Outro | 4.5 s | The install command, ulgen.mefby.com, github.com/mftnakrsu/ulgen |

## Words, captions and audio

- Every word the video itself puts on screen (headlines, the key lines, the outro's labels) lives
  in `src/i18n.ts`, one dictionary per language. What the terminal shows is program output and
  lives in `src/data/en.ts` and `src/data/tr.ts`, one file per language, so it can be swapped.
- The subtitles are one timed script per language in `src/captions.ts` (start and end in ms; the
  scene times are in the comment above it). They are burned into the captioned and voiced renders
  and exported as `.srt`, `.vtt` and `.json` (`npm run render:captions`).
- `UlgenDemo` takes the props of the Oyla and Flashjot demos: `captions` (off by default; on, the
  scenes shrink to 90 % from the top to make room), `musicSrc`, `musicVolume`, `musicDuckedVolume`,
  `voiceoverSrc`, `voiceoverClips` and `voiceoverVolume` (`src/components/Soundtrack.tsx`). Without
  a source the video stays silent.
- The voiced renders use one voiceover clip per caption cue,
  `public/audio/voiceover/ulgen-demo-<lang>/01.wav`, `02.wav`, …, each played from the start of
  its cue and cut at its end. The clips are made from the exported `.json` cues by a separate
  voiceover script that is not in this repository (Chatterbox Multilingual with the same fixed
  voice as Oyla and Flashjot; best of several takes, checked with Whisper, trimmed, -16 LUFS). A
  cue's optional `say` is what the voice reads instead of the subtitle ("Evren" for "EVREN", which
  would be spelled out; "Ulgen" for "Ülgen" in English; "ulgen dot mefby dot com"), only a
  respelling: the voice says the same words as the subtitle. Every cue is voiced, and each clip fits
  inside its cue; a line too long for that is shortened in `text`. After changing a line, re-export
  the captions, regenerate its clip and check that it still fits its cue.
- The music of the voiced renders is the `MUSIC` constant in `scripts/render.mjs` (a file in
  `public/audio/`, CC0, see `public/audio/LICENSES.md`): 0.5 between lines, ducked to 0.25 under
  the voice, faded in over 0.5 s and out over the last 1.5 s.

## Where the text comes from

The terminal text was captured on 2026-10-23 from ulgen 0.2.0 (the release build,
`ulgen_0.2.0_linux_amd64.tar.gz`), signed in to EVREN, with Claude Code 2.1.309 and opencode 2.0.28
installed, through a pseudo-terminal of 108 × 20 characters (the size of the video's terminal),
with `LANG=en_US.UTF-8` and `LANG=tr_TR.UTF-8`. The scripts are in `scripts/capture/`:

- `capture-cli.py` runs `ulgen setup` (the list), `ulgen setup opencode --dry-run`,
  `ulgen setup opencode`, `ulgen doctor` and `ulgen status` with HOME in a sandbox under
  `/tmp/ulgen-demo`, where opencode.json starts with only `$schema` and `theme`, so setup shows an
  update and its backup. ulgen prints no colours, and the video adds none to its output.
- `capture-claude.py` records `ulgen claude` the way `docs/assets/demo.tape` sets it up: the
  project `/tmp/ulgen-demo/hello` with the small `main.go`, Claude Code's own configuration in
  `CLAUDE_CONFIG_DIR=/tmp/ulgen-demo/cfg`, a clean environment. The screen is kept with
  [pyte](https://github.com/selectel/pyte), cell by cell with its colours. The English question was
  answered on the first run; the first Turkish answer first named 10946 and then corrected itself,
  so the second Turkish run is used.
- `make-data.py` writes `src/data/en.ts` and `src/data/tr.ts` from those captures (run
  `npx prettier --write --print-width 160 src/data/*.ts` after it). The Claude Code strings in it
  are written in by hand and checked against the captured screens; the script stops at one that
  does not appear exactly.

What is not a capture:

- `ulgen login` is not run for the video: it would need a key pasted on camera. Its lines are the
  message catalog's (`internal/i18n/locales/`), in the order `cmd/ulgen/login.go` prints them, for
  an account that has not accepted the terms yet; the status lines after it are the captured
  `ulgen status`. The terms text itself (Turkish, about 3,500 characters) is drawn as grey bars,
  and the time of acceptance is a minute before the setup's backup.
- The pace: output that took seconds to arrive comes faster (Claude Code took 18 s to answer in
  English, 14 s in Turkish), and the 128 lines of the dry run's diff scroll by in about a second
  instead of appearing at once. Claude Code's counters show the values captured along the way.
- Claude Code's screen is redrawn, not filmed: its text, rows and colours are the captured ones; the
  glyphs JetBrains Mono does not have (⎿, ⏵, the spinner's stars) and the mascot's block
  elements are drawn as SVG.

No key, user name, host name or home directory is in any of it; paths are shown as ulgen prints
them (`~/…`). The raw captures are not in the repository.

To refresh the text after ulgen's output changed: run the capture scripts (their docstrings say
how; `capture-claude.py ask` costs a little EVREN quota), update `CLAUDE` in `make-data.py` to what
the new screens show, run it, and check the scenes in the Studio. If a line grows past 108
characters it wraps, as in a real terminal.

## Commands

```bash
npm install
npm run dev                # Remotion Studio, to preview and tweak scenes
npm run render             # captions, both demos, posters, gallery stills, captioned demos → out/
npm run render:demo:en     # or render:demo:tr, render:demo (both)
npm run render:demo:captioned
npm run render:posters
npm run render:stills      # out/ulgen-{doctor,status,setup,claude}.png
npm run render:voiced      # both demos with subtitles, voiceover and music → out/*-voiced.mp4
npm run render:captions    # only the .srt/.vtt/.json files (Node 22.18+, which runs TypeScript directly)
npm run lint               # eslint and tsc
```

`npm run render` never writes outside `out/`. It bundles the project once, then renders each output
in turn with two browser tabs; add `-- --concurrency=8` on a machine with more memory.

This directory holds no Go code, and `node_modules/` and `out/` are not committed. After
`npm install`, though, `node_modules` holds one `.go` file of a dependency (flatted); `video/go.mod`
makes this directory a module of its own, so `go build ./...`, `go vet ./...` and `go test ./...`
in the repository stop at it.

## License

The code is AGPL-3.0-only like the rest of Ülgen. Remotion itself has its own license: free for
individuals and companies of up to three people; see
[remotion.pro/license](https://www.remotion.pro/license). The fonts in `public/fonts` are under
the SIL Open Font License, with their license files next to them: JetBrains Mono 2.304 (the
terminal, as in the recordings vhs makes) and Atkinson Hyperlegible Next (the headlines and
subtitles). The music is CC0 and the voiceover is AI-generated; see `public/audio/LICENSES.md`.
