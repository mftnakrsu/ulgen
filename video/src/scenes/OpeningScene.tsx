import { Easing, Interactive, interpolate, useCurrentFrame, useVideoConfig } from "remotion";
import { Logo } from "../components/Logo";
import { Stage } from "../components/Stage";
import { copy, type Lang } from "../i18n";
import { clamp, colors } from "../theme";

/** The logo draws itself, the name slides in, then the README's tagline. */
const OpeningSceneInner = ({ lang }: { readonly lang: Lang }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  return (
    <Stage>
      <div style={{ position: "absolute", inset: 0, display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 48 }}>
          <Logo
            size={250}
            draw={interpolate(frame, [0.1 * fps, 0.9 * fps], [0, 1], { ...clamp, easing: Easing.bezier(0.45, 0, 0.2, 1) })}
            dots={interpolate(frame, [0.6 * fps, 1.1 * fps], [0, 1], { ...clamp, easing: Easing.bezier(0.2, 0.9, 0.3, 1.3) })}
          />
          <div
            style={{
              fontSize: 200,
              fontWeight: 800,
              letterSpacing: "-0.03em",
              color: colors.ink,
              opacity: interpolate(frame, [0.8 * fps, 1.2 * fps], [0, 1], clamp),
              translate: interpolate(frame, [0.8 * fps, 1.3 * fps], ["40px 0px", "0px 0px"], { ...clamp, easing: Easing.bezier(0.16, 1, 0.3, 1) }),
            }}
          >
            Ülgen
          </div>
        </div>
        <div
          style={{
            marginTop: 56,
            fontSize: 54,
            color: colors.muted,
            opacity: interpolate(frame, [1.4 * fps, 1.9 * fps], [0, 1], clamp),
            translate: interpolate(frame, [1.4 * fps, 1.9 * fps], ["0px 14px", "0px 0px"], { ...clamp, easing: Easing.bezier(0.16, 1, 0.3, 1) }),
          }}
        >
          {copy[lang].opening.tagline}
        </div>
      </div>
    </Stage>
  );
};

export const OpeningScene = Interactive.create({ Component: OpeningSceneInner, layout: "absolute-fill" });
