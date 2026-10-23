import { Audio } from "@remotion/media";
import type React from "react";
import { interpolate, staticFile, useCurrentFrame, useVideoConfig } from "remotion";
import type { Cue } from "../captions";
import { clamp } from "../theme";

/**
 * Optional audio for the demo. Every track is off unless a source is given,
 * so by default the videos stay silent. A source is a file in public/ (e.g. "audio/music.mp3") or
 * a URL. scripts/render.mjs sets these props for the "voiced" renders.
 */
export type SoundtrackProps = {
  readonly musicSrc?: string;
  /** 0–1, default 0.4. The music fades in over 0.5 s and out over the last 1.5 s. */
  readonly musicVolume?: number;
  /**
   * 0–1, the music volume while a voiceover line plays (default: half of `musicVolume`). With a
   * voiceover the music dips under every caption cue and comes back up between scenes.
   */
  readonly musicDuckedVolume?: number;
  /** A single voiceover file, starting at frame 0. */
  readonly voiceoverSrc?: string;
  /**
   * A folder in public/ with one voiceover clip per caption cue, `01.wav`, `02.wav`, … in cue
   * order; each plays from the start of its cue and is cut at its end. Made from the exported
   * captions by a separate voiceover script, not in this repo, e.g. "audio/voiceover/ulgen-demo-en".
   */
  readonly voiceoverClips?: string;
  /** 0–1, default 1. */
  readonly voiceoverVolume?: number;
};

const resolve = (src: string) => (/^(https?:|data:|blob:)/.test(src) ? src : staticFile(src));

/** The music starts dipping this long before a line and comes back up this long after it. */
const DUCK_ATTACK_MS = 250;
const DUCK_RELEASE_MS = 500;
/** Lines closer together than this share one dip, so the music does not pump between them. */
const DUCK_BRIDGE_MS = 1000;

/** How far the music is ducked at `ms`: 0 = full volume, 1 = fully ducked. */
const duckAmount = (ms: number, cues: readonly Cue[]) => {
  const spans: { startMs: number; endMs: number }[] = [];
  for (const cue of cues) {
    const last = spans[spans.length - 1];
    if (last && cue.startMs - last.endMs < DUCK_BRIDGE_MS) last.endMs = cue.endMs;
    else spans.push({ startMs: cue.startMs, endMs: cue.endMs });
  }
  return Math.max(
    0,
    ...spans.map((span) =>
      interpolate(ms, [span.startMs - DUCK_ATTACK_MS, span.startMs, span.endMs, span.endMs + DUCK_RELEASE_MS], [0, 1, 1, 0], clamp),
    ),
  );
};

export const Soundtrack: React.FC<SoundtrackProps & { readonly cues: readonly Cue[] }> = ({
  cues,
  musicSrc,
  musicVolume = 0.4,
  musicDuckedVolume = musicVolume / 2,
  voiceoverSrc,
  voiceoverClips,
  voiceoverVolume = 1,
}) => {
  const frame = useCurrentFrame();
  const { fps, durationInFrames } = useVideoConfig();
  const voiced = Boolean(voiceoverSrc || voiceoverClips);
  const duck = voiced ? duckAmount((frame / fps) * 1000, cues) : 0;
  const toFrame = (ms: number) => Math.round((ms / 1000) * fps);
  return (
    <>
      {musicSrc ? (
        <Audio
          name="Music"
          src={resolve(musicSrc)}
          volume={
            interpolate(duck, [0, 1], [musicVolume, musicDuckedVolume]) *
            interpolate(frame, [0, 0.5 * fps, durationInFrames - 1.5 * fps, durationInFrames], [0, 1, 1, 0], clamp)
          }
        />
      ) : null}
      {voiceoverSrc ? <Audio name="Voiceover" src={resolve(voiceoverSrc)} volume={voiceoverVolume} /> : null}
      {voiceoverClips
        ? cues.map((cue, index) => (
            <Audio
              key={cue.startMs}
              name={`Voice ${index + 1}`}
              src={resolve(`${voiceoverClips}/${String(index + 1).padStart(2, "0")}.wav`)}
              from={toFrame(cue.startMs)}
              durationInFrames={toFrame(cue.endMs) - toFrame(cue.startMs)}
              volume={voiceoverVolume}
            />
          ))
        : null}
    </>
  );
};
