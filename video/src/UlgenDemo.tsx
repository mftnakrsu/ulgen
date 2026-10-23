import { linearTiming, TransitionSeries } from "@remotion/transitions";
import { fade } from "@remotion/transitions/fade";
import { AbsoluteFill, useVideoConfig } from "remotion";
import { captionScripts } from "./captions";
import { Soundtrack, type SoundtrackProps } from "./components/Soundtrack";
import { Subtitles } from "./components/Subtitles";
import type { Lang } from "./i18n";
import { ClaudeScene } from "./scenes/ClaudeScene";
import { HealthScene } from "./scenes/HealthScene";
import { KeyScene } from "./scenes/KeyScene";
import { LoginScene } from "./scenes/LoginScene";
import { OpeningScene } from "./scenes/OpeningScene";
import { OutroScene } from "./scenes/OutroScene";
import { SetupScene } from "./scenes/SetupScene";
import { colors } from "./theme";

export type UlgenDemoProps = SoundtrackProps & {
  readonly lang: Lang;
  /** Burned-in subtitles from src/captions.ts. Off renders the caption-free demo. */
  readonly captions?: boolean;
};

/** With subtitles, the scenes shrink to 90 % from the top centre, freeing the bottom 108 px. */
const CAPTIONED_STAGE = 0.9;

/**
 * 40.5 s product demo: 7 scenes with 0.5 s fades between them
 * (3.5 + 6.5 + 7 + 7.5 + 10 + 4.5 + 4.5 s, minus 6 × 0.5 s = 1215 frames at 30 fps).
 */
export const UlgenDemo: React.FC<UlgenDemoProps> = ({ lang, captions = false, ...soundtrack }) => {
  const { fps } = useVideoConfig();
  const scenes = (
    <TransitionSeries>
      <TransitionSeries.Sequence name="Opening" durationInFrames={3.5 * fps}>
        <OpeningScene lang={lang} />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition presentation={fade()} timing={linearTiming({ durationInFrames: 0.5 * fps })} />
      <TransitionSeries.Sequence name="Login" durationInFrames={6.5 * fps}>
        <LoginScene lang={lang} />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition presentation={fade()} timing={linearTiming({ durationInFrames: 0.5 * fps })} />
      <TransitionSeries.Sequence name="Setup" durationInFrames={7 * fps}>
        <SetupScene lang={lang} />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition presentation={fade()} timing={linearTiming({ durationInFrames: 0.5 * fps })} />
      <TransitionSeries.Sequence name="Health" durationInFrames={7.5 * fps}>
        <HealthScene lang={lang} />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition presentation={fade()} timing={linearTiming({ durationInFrames: 0.5 * fps })} />
      <TransitionSeries.Sequence name="Claude Code" durationInFrames={10 * fps}>
        <ClaudeScene lang={lang} />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition presentation={fade()} timing={linearTiming({ durationInFrames: 0.5 * fps })} />
      <TransitionSeries.Sequence name="Key" durationInFrames={4.5 * fps}>
        <KeyScene lang={lang} />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition presentation={fade()} timing={linearTiming({ durationInFrames: 0.5 * fps })} />
      <TransitionSeries.Sequence name="Outro" durationInFrames={4.5 * fps}>
        <OutroScene lang={lang} />
      </TransitionSeries.Sequence>
    </TransitionSeries>
  );
  return (
    <AbsoluteFill style={{ backgroundColor: colors.page }}>
      {captions ? (
        <>
          <AbsoluteFill style={{ scale: CAPTIONED_STAGE, transformOrigin: "50% 0%" }}>{scenes}</AbsoluteFill>
          <Subtitles cues={captionScripts.demo.cues[lang]} fontSize={42} maxWidth={1560} bottom={30} />
        </>
      ) : (
        scenes
      )}
      <Soundtrack cues={captionScripts.demo.cues[lang]} {...soundtrack} />
    </AbsoluteFill>
  );
};
