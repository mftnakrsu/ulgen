#!/usr/bin/env node
/**
 * Writes the subtitle scripts of src/captions.ts as SubRip (.srt) and WebVTT (.vtt) files, one per
 * composition and language, into out/captions/ (or the directory given as the first argument):
 *
 *   node scripts/export-captions.mjs [out-dir]   →  ulgen-demo-en.srt, ulgen-demo-en.vtt, …
 *
 * The same lines are burned into the videos, so a voiceover generated from these files matches
 * what is on screen. A .json copy of each script (with the voiceover's respellings, `say`) feeds the
 * voiceover generator. Needs Node 22.18+ (or 23.6+), which can import TypeScript without a flag.
 */
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const video = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const { captionScripts } = await import(join(video, "src", "captions.ts"));
const outDir = resolve(process.argv[2] ?? join(video, "out", "captions"));

const pad = (value, length = 2) => String(value).padStart(length, "0");
/** 00:00:03,400 for SRT, 00:00:03.400 for VTT. */
const timestamp = (ms, separator) => {
  const hours = Math.floor(ms / 3_600_000);
  const minutes = Math.floor((ms % 3_600_000) / 60_000);
  const seconds = Math.floor((ms % 60_000) / 1000);
  return `${pad(hours)}:${pad(minutes)}:${pad(seconds)}${separator}${pad(ms % 1000, 3)}`;
};

const toSrt = (cues) =>
  cues.map((cue, index) => `${index + 1}\n${timestamp(cue.startMs, ",")} --> ${timestamp(cue.endMs, ",")}\n${cue.text}\n`).join("\n");

const toVtt = (cues) =>
  "WEBVTT\n\n" +
  cues.map((cue, index) => `${index + 1}\n${timestamp(cue.startMs, ".")} --> ${timestamp(cue.endMs, ".")}\n${cue.text}\n`).join("\n");

/** Catch mistakes in the script before they reach a subtitle file or a voiceover. */
const check = (name, script, lang) => {
  let previousEnd = 0;
  for (const cue of script.cues[lang]) {
    const where = `${name}/${lang} "${cue.text}"`;
    if (!(cue.startMs < cue.endMs)) throw new Error(`${where}: ends before it starts`);
    if (cue.startMs < previousEnd) throw new Error(`${where}: overlaps the cue before it`);
    if (cue.endMs > script.durationMs) throw new Error(`${where}: runs past the end of the video`);
    previousEnd = cue.endMs;
  }
};

mkdirSync(outDir, { recursive: true });
for (const [name, script] of Object.entries(captionScripts)) {
  for (const [lang, cues] of Object.entries(script.cues)) {
    check(name, script, lang);
    const base = join(outDir, `${script.file}-${lang}`);
    writeFileSync(`${base}.srt`, toSrt(cues));
    writeFileSync(`${base}.vtt`, toVtt(cues));
    // The cues as data for the voiceover generator (a separate voiceover script, not in this repo), with the
    // spoken respellings (`say`) that the subtitle files leave out.
    writeFileSync(`${base}.json`, `${JSON.stringify({ file: script.file, lang, durationMs: script.durationMs, cues }, null, 1)}\n`);
    console.log(`${base}.srt, .vtt, .json (${cues.length} cues)`);
  }
}
