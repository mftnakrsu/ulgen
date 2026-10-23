import { Easing, Interactive, interpolate, useCurrentFrame, useVideoConfig } from "remotion";
import { Logo } from "../components/Logo";
import { Stage } from "../components/Stage";
import { copy, type Lang } from "../i18n";
import { clamp, colors, monoFamily } from "../theme";

/** The install command of the README (Linux and macOS). */
const INSTALL = "curl -fsSL https://raw.githubusercontent.com/mftnakrsu/ulgen/main/scripts/install.sh | sh";

/** The name again, how to install it, where the docs and the code are. */
const OutroSceneInner = ({ lang }: { readonly lang: Lang }) => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const text = copy[lang].outro;
  const label = { fontSize: 40, color: colors.muted, textAlign: "right" } as const;
  const value = { fontSize: 56, fontWeight: 800, color: colors.ink } as const;
  return (
    <Stage>
      <div style={{ position: "absolute", inset: 0, display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 36 }}>
          <Logo size={170} dots={interpolate(frame, [0.05 * fps, 0.45 * fps], [0.4, 1], { ...clamp, easing: Easing.bezier(0.2, 0.9, 0.3, 1.3) })} />
          <div style={{ fontSize: 150, fontWeight: 800, letterSpacing: "-0.03em", color: colors.ink }}>Ülgen</div>
        </div>
        <div
          style={{
            marginTop: 64,
            display: "grid",
            gridTemplateColumns: "auto auto",
            columnGap: 36,
            rowGap: 22,
            alignItems: "baseline",
            opacity: interpolate(frame, [0.5 * fps, 1.0 * fps], [0, 1], clamp),
            translate: interpolate(frame, [0.5 * fps, 1.0 * fps], ["0px 24px", "0px 0px"], { ...clamp, easing: Easing.bezier(0.16, 1, 0.3, 1) }),
          }}
        >
          <div style={label}>{text.install}</div>
          <div
            style={{
              fontFamily: monoFamily,
              fontSize: 27,
              color: colors.terminalText,
              backgroundColor: colors.terminal,
              border: `1px solid ${colors.border}`,
              borderRadius: 12,
              padding: "14px 22px",
              whiteSpace: "pre",
              fontVariantLigatures: "none",
            }}
          >
            <span style={{ color: colors.terminalDim }}>$ </span>
            {INSTALL}
          </div>
          <div style={label}>{text.docs}</div>
          <div style={value}>ulgen.mefby.com</div>
          <div style={label}>{text.code}</div>
          <div style={value}>github.com/mftnakrsu/ulgen</div>
        </div>
      </div>
    </Stage>
  );
};

export const OutroScene = Interactive.create({ Component: OutroSceneInner, layout: "absolute-fill" });
