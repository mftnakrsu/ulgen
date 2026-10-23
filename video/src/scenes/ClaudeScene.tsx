import { Interactive, useCurrentFrame } from "remotion";
import { ClaudeCodeScreen } from "../components/ClaudeCode";
import { TerminalSession } from "../components/Terminal";
import { demoData } from "../data";
import { copy, type Lang } from "../i18n";
import { WINDOW } from "./layout";
import { CLAUDE_UI_AT, claudeShellSteps, claudeTiming } from "./sessions";
import { TerminalScene } from "./TerminalScene";

/**
 * `ulgen claude` in /tmp/ulgen-demo/hello: ulgen's notice, then Claude Code on an EVREN model is
 * asked what main.go prints, reads the file and answers. The screen is redrawn from the captured
 * session at the size it was captured at (108 × 20).
 */
const ClaudeSceneInner = ({ lang }: { readonly lang: Lang }) => {
  const frame = useCurrentFrame();
  const data = demoData[lang];
  return (
    <TerminalScene command={copy[lang].claude.command} title={copy[lang].claude.title}>
      {frame < CLAUDE_UI_AT ? (
        <TerminalSession steps={claudeShellSteps(data)} rows={WINDOW.rows} cols={WINDOW.cols} fontSize={WINDOW.fontSize} lineHeight={WINDOW.lineHeight} />
      ) : (
        <ClaudeCodeScreen
          data={data.claude}
          timing={claudeTiming}
          cols={WINDOW.cols}
          rows={WINDOW.rows}
          fontSize={WINDOW.fontSize}
          lineHeight={WINDOW.lineHeight}
          highlightAnswer
        />
      )}
    </TerminalScene>
  );
};

export const ClaudeScene = Interactive.create({ Component: ClaudeSceneInner, layout: "absolute-fill" });
