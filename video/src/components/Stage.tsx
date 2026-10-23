import type React from "react";
import { AbsoluteFill, Easing, interpolate, useCurrentFrame, useVideoConfig } from "remotion";
import { clamp, colors, fontFamily, monoFamily } from "../theme";

/** The dark page every scene sits on, with a faint glow of the accent behind the window. */
export const Stage: React.FC<{ readonly children: React.ReactNode }> = ({ children }) => (
  <AbsoluteFill
    style={{
      backgroundColor: colors.page,
      backgroundImage: `radial-gradient(ellipse 70% 55% at 50% 42%, ${colors.glow}, transparent 70%)`,
      fontFamily,
    }}
  >
    {children}
  </AbsoluteFill>
);

type HeadlineProps = {
  /** The command the scene is about, in the terminal font. */
  readonly command: string;
  readonly title: string;
  readonly fontSize: number;
  readonly style?: React.CSSProperties;
};

/** A scene's headline: the command in a pill, then one line of text. Slides in at the start. */
export const Headline: React.FC<HeadlineProps> = ({ command, title, fontSize, style }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  return (
    <div
      style={{
        position: "absolute",
        display: "flex",
        alignItems: "center",
        gap: fontSize * 0.5,
        opacity: interpolate(frame, [0.1 * fps, 0.5 * fps], [0, 1], clamp),
        translate: interpolate(frame, [0.1 * fps, 0.6 * fps], ["0px 16px", "0px 0px"], { ...clamp, easing: Easing.bezier(0.16, 1, 0.3, 1) }),
        ...style,
      }}
    >
      <div
        style={{
          fontFamily: monoFamily,
          fontSize: fontSize * 0.56,
          fontWeight: 600,
          color: colors.accent,
          padding: `${fontSize * 0.12}px ${fontSize * 0.32}px`,
          borderRadius: fontSize * 0.2,
          border: `2px solid rgba(47, 129, 247, 0.55)`,
          backgroundColor: "rgba(47, 129, 247, 0.1)",
          whiteSpace: "nowrap",
          fontVariantLigatures: "none",
        }}
      >
        {command}
      </div>
      <div style={{ fontSize, fontWeight: 800, letterSpacing: "-0.01em", color: colors.ink, whiteSpace: "nowrap" }}>{title}</div>
    </div>
  );
};
