import { ClaudeCodeScreen } from "../components/ClaudeCode";
import { Stage } from "../components/Stage";
import { rowsFor, type Step, TERMINAL_PADDING, TerminalSession, TerminalWindow, TITLE_BAR } from "../components/Terminal";
import { demoData } from "../data";
import type { Lang } from "../i18n";
import { claudeTiming, doctorStill, setupStill, statusStill } from "../scenes/sessions";

export type GalleryStillProps = {
  readonly which: "doctor" | "status" | "setup" | "claude";
  readonly lang: Lang;
};

/** The still's terminal: 2040 px wide, large type; tall enough for its rows. */
const WIDTH = 2040;
const FONT_SIZE = 32;
const LINE_HEIGHT = 46;
const COLS = Math.floor((WIDTH - 2 - 2 * TERMINAL_PADDING * FONT_SIZE) / (0.6 * FONT_SIZE));
const heightFor = (rows: number) => Math.ceil((TITLE_BAR + 2 * TERMINAL_PADDING) * FONT_SIZE + rows * LINE_HEIGHT + 2);

/** Done long before this frame: the stills show the end of each session. */
const END = 100_000;

/**
 * A key terminal state for the gallery of the website's product page (2160 × 1350): the window
 * alone on the page, at the end of the session, with the prompt back.
 */
export const GalleryStill: React.FC<GalleryStillProps> = ({ which, lang }) => {
  const data = demoData[lang];
  const sessions: Record<Exclude<GalleryStillProps["which"], "claude">, Step[]> = {
    doctor: doctorStill(data),
    status: statusStill(data),
    setup: setupStill(data),
  };
  // Lines of each session, plus the prompt; the Claude Code screen keeps its captured 20 rows.
  const rows = which === "claude" ? 20 : { doctor: 12, status: 14, setup: 16 }[which];
  const height = heightFor(rows);
  return (
    <Stage>
      <div style={{ position: "absolute", inset: 0, display: "flex", alignItems: "center", justifyContent: "center" }}>
        <TerminalWindow width={WIDTH} height={height} fontSize={FONT_SIZE}>
          {which === "claude" ? (
            <ClaudeCodeScreen data={data.claude} timing={claudeTiming} cols={COLS} rows={rows} fontSize={FONT_SIZE} lineHeight={LINE_HEIGHT} frozenAt={END} />
          ) : (
            <TerminalSession
              steps={sessions[which]}
              rows={rowsFor(height - 2, FONT_SIZE, LINE_HEIGHT)}
              cols={COLS}
              fontSize={FONT_SIZE}
              lineHeight={LINE_HEIGHT}
              frozenAt={END}
            />
          )}
        </TerminalWindow>
      </div>
    </Stage>
  );
};
