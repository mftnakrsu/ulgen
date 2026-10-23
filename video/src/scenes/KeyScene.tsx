import type React from "react";
import { Easing, Interactive, interpolate, useCurrentFrame, useVideoConfig } from "remotion";
import { Stage } from "../components/Stage";
import { copy, type Lang } from "../i18n";
import { clamp, colors } from "../theme";

/** A key, drawn like the logo: round strokes in the accent blue. */
const KeyIcon: React.FC<{ readonly size: number }> = ({ size }) => (
  <svg viewBox="0 0 64 64" width={size} height={size}>
    <g fill="none" stroke={colors.accent} strokeWidth={5} strokeLinecap="round" strokeLinejoin="round">
      <circle cx={22} cy={32} r={11} />
      <path d="M33 32h22M47 32v8M54 32v6" />
    </g>
  </svg>
);

const Check: React.FC<{ readonly size: number }> = ({ size }) => (
  <svg viewBox="0 0 24 24" width={size} height={size} style={{ flexShrink: 0 }}>
    <circle cx={12} cy={12} r={11} fill="rgba(47, 129, 247, 0.16)" />
    <path d="M7 12.5l3.2 3.2L17 8.8" fill="none" stroke={colors.accent} strokeWidth={2.2} strokeLinecap="round" strokeLinejoin="round" />
  </svg>
);

/** Three lines of the README's "Your key stays yours", one after the other. */
const KeySceneInner = ({ lang }: { readonly lang: Lang }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const text = copy[lang].key;
  const appear = (at: number): React.CSSProperties => ({
    opacity: interpolate(frame, [at * fps, (at + 0.4) * fps], [0, 1], clamp),
    translate: interpolate(frame, [at * fps, (at + 0.5) * fps], ["0px 18px", "0px 0px"], { ...clamp, easing: Easing.bezier(0.16, 1, 0.3, 1) }),
  });
  return (
    <Stage>
      <div style={{ position: "absolute", inset: 0, display: "flex", alignItems: "center", justifyContent: "center" }}>
        <div>
          <div style={{ display: "flex", alignItems: "center", gap: 28, ...appear(0.15) }}>
            <KeyIcon size={92} />
            <div style={{ fontSize: 88, fontWeight: 800, letterSpacing: "-0.02em", color: colors.ink }}>{text.title}</div>
          </div>
          <div style={{ marginTop: 56, display: "flex", flexDirection: "column", gap: 30 }}>
            {text.points.map((point, i) => (
              <div key={point} style={{ display: "flex", alignItems: "center", gap: 26, fontSize: 50, color: colors.ink, ...appear(0.6 + 0.45 * i) }}>
                <Check size={54} />
                {point}
              </div>
            ))}
          </div>
        </div>
      </div>
    </Stage>
  );
};

export const KeyScene = Interactive.create({ Component: KeySceneInner, layout: "absolute-fill" });
