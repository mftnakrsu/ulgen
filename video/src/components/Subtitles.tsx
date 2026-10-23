import type React from "react";
import { interpolate, useCurrentFrame, useVideoConfig } from "remotion";
import type { Cue } from "../captions";
import { clamp, colors, fontFamily } from "../theme";

type SubtitlesProps = {
  readonly cues: readonly Cue[];
  readonly fontSize: number;
  /** Widest the subtitle box may get; longer lines wrap. */
  readonly maxWidth: number;
  /** Distance from the bottom of the frame to the bottom of the box. */
  readonly bottom: number;
};

/** Fade in and out over this long, inside each cue's own time. */
const FADE_MS = 120;

/** Burned-in subtitles: the cue playing now, as light text on a dark box centred near the bottom. */
export const Subtitles: React.FC<SubtitlesProps> = ({ cues, fontSize, maxWidth, bottom }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const ms = (frame / fps) * 1000;
  const cue = cues.find((c) => ms >= c.startMs && ms < c.endMs);
  if (!cue) return null;
  return (
    <div
      style={{
        position: "absolute",
        left: 0,
        right: 0,
        bottom,
        display: "flex",
        justifyContent: "center",
        pointerEvents: "none",
        opacity: interpolate(ms, [cue.startMs, cue.startMs + FADE_MS, cue.endMs - FADE_MS, cue.endMs], [0, 1, 1, 0], clamp),
        translate: interpolate(ms, [cue.startMs, cue.startMs + 2 * FADE_MS], ["0px 12px", "0px 0px"], clamp),
      }}
    >
      <div
        style={{
          maxWidth,
          padding: `${fontSize * 0.28}px ${fontSize * 0.6}px`,
          borderRadius: fontSize * 0.4,
          backgroundColor: "rgba(22, 27, 34, 0.92)",
          border: `1px solid ${colors.border}`,
          color: colors.ink,
          fontFamily,
          fontSize,
          fontWeight: 600,
          lineHeight: 1.25,
          textAlign: "center",
          textWrap: "balance",
        }}
      >
        {cue.text}
      </div>
    </div>
  );
};
