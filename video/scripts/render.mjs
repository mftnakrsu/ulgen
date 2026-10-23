#!/usr/bin/env node
/**
 * Renders the Ülgen demo, its posters, the gallery stills and the subtitle files into out/:
 *
 *   node scripts/render.mjs [all | demo | demo-en | demo-tr | demo-captioned | posters | stills | captions
 *                            | voiced | demo-voiced-en | demo-voiced-tr] ... [--concurrency=N]
 *
 *   out/ulgen-demo-<lang>.mp4             1920×1080, 30 fps, H.264, no audio track
 *   out/ulgen-demo-<lang>-captioned.mp4   the same with the subtitles burned in
 *   out/ulgen-demo-<lang>-poster.png      a representative frame (Claude Code's answer)
 *   out/ulgen-<doctor|status|setup|claude>.png   2160×1350 stills for the website's gallery
 *   out/captions/ulgen-demo-<lang>.{srt,vtt,json}   the subtitles, and the voiceover's input
 *
 * and, only when asked for (`voiced`, `demo-voiced-<lang>`; not part of `all`):
 *
 *   out/ulgen-demo-<lang>-voiced.mp4      subtitles, voiceover and music, AAC audio
 *
 * The voiced renders need the voiceover clips in public/audio/voiceover/ulgen-demo-<lang>/ (one
 * per caption cue, made from out/captions/*.json by a voiceover script that is not in this repo)
 * and the music in public/audio/ (MUSIC).
 *
 * Nothing outside out/ is written. The project is bundled once, then each output is rendered in
 * turn with two browser tabs (--concurrency=N uses more on a bigger machine).
 */
import { spawnSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const video = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const out = join(video, "out");
const bundle = join(out, "bundle");

const LANGS = ["en", "tr"];
const STILLS = ["doctor", "status", "setup", "claude"];
/**
 * Background music of the voiced renders, a file in public/. CC0 (Loyalty Freak Music, see
 * public/audio/LICENSES.md). To use another track, copy it into public/audio/ and change it here.
 */
const MUSIC = "audio/01_LoyaltyFreakMusic_One-Cool-Minute_CC0.mp3";
/** A representative frame: Claude Code has answered through EVREN. */
const POSTER_FRAME = 930;

const args = process.argv.slice(2);
const options = args.filter((arg) => arg.startsWith("--"));
const requested = args.filter((arg) => !arg.startsWith("--"));
const concurrency = options.find((flag) => flag.startsWith("--concurrency="))?.split("=")[1] ?? "2";
const unknownOptions = options.filter((flag) => !flag.startsWith("--concurrency="));
if (unknownOptions.length > 0) {
  console.error(`Unknown option: ${unknownOptions.join(", ")}. Use --concurrency=N.`);
  process.exit(1);
}

const run = (command, commandArgs) => {
  console.log(`\n> ${command} ${commandArgs.join(" ")}`);
  const result = spawnSync(command, commandArgs, { cwd: video, stdio: "inherit" });
  if (result.status !== 0) process.exit(result.status ?? 1);
};
const remotion = (...args) => run("npx", ["remotion", ...args]);

const render = (id, file, ...flags) => remotion("render", bundle, id, join(out, file), `--concurrency=${concurrency}`, ...flags);
/**
 * H.264, yuv420p tagged as BT.709, no audio track. Frames are captured as PNG: from JPEG frames
 * the encoder would fall back to full-range yuvj420p and the colours would drift.
 */
const h264 = (crf) => ["--codec=h264", `--crf=${crf}`, "--image-format=png", "--pixel-format=yuv420p", "--color-space=bt709", "--muted"];
/** The same encoding with an AAC audio track: voiceover clips and music, ducked under the voice. */
const voiced = (lang) => [
  ...h264(20).filter((flag) => flag !== "--muted"),
  "--audio-codec=aac",
  "--audio-bitrate=192k",
  `--props=${JSON.stringify({ captions: true, musicSrc: MUSIC, musicVolume: 0.5, musicDuckedVolume: 0.25, voiceoverClips: `audio/voiceover/ulgen-demo-${lang}` })}`,
];
const still = (id, file, frame) => remotion("still", bundle, id, join(out, file), `--frame=${frame}`, "--image-format=png");

const jobs = {
  "demo-en": () => render("UlgenDemo-en", "ulgen-demo-en.mp4", ...h264(20)),
  "demo-tr": () => render("UlgenDemo-tr", "ulgen-demo-tr.mp4", ...h264(20)),
  /** The demo with the subtitles burned in. */
  "demo-captioned": () => {
    for (const lang of LANGS) {
      render(`UlgenDemo-${lang}`, `ulgen-demo-${lang}-captioned.mp4`, ...h264(20), `--props=${JSON.stringify({ captions: true })}`);
    }
  },
  "demo-voiced-en": () => render("UlgenDemo-en", "ulgen-demo-en-voiced.mp4", ...voiced("en")),
  "demo-voiced-tr": () => render("UlgenDemo-tr", "ulgen-demo-tr-voiced.mp4", ...voiced("tr")),
  posters: () => {
    for (const lang of LANGS) still(`UlgenDemo-${lang}`, `ulgen-demo-${lang}-poster.png`, POSTER_FRAME);
  },
  /** The gallery stills of the website's product page, from the still-<name> compositions. */
  stills: () => {
    for (const name of STILLS) still(`still-${name}`, `ulgen-${name}.png`, 0);
  },
  /** The subtitle script as .srt, .vtt and .json, e.g. out/captions/ulgen-demo-tr.srt. */
  captions: () =>
    run(process.execPath, ["--disable-warning=ExperimentalWarning", join(video, "scripts", "export-captions.mjs"), join(out, "captions")]),
};
jobs.demo = () => {
  jobs["demo-en"]();
  jobs["demo-tr"]();
};
jobs.voiced = () => {
  for (const lang of LANGS) jobs[`demo-voiced-${lang}`]();
};
jobs.all = () => {
  jobs.captions();
  jobs.demo();
  jobs.posters();
  jobs.stills();
  jobs["demo-captioned"]();
};

const unknown = requested.filter((name) => !(name in jobs));
if (unknown.length > 0) {
  console.error(`Unknown job: ${unknown.join(", ")}. Use: ${Object.keys(jobs).join(", ")}`);
  process.exit(1);
}

const names = requested.length > 0 ? requested : ["all"];
if (names.some((name) => name !== "captions")) remotion("bundle", `--out-dir=${bundle}`);
for (const name of names) jobs[name]();
