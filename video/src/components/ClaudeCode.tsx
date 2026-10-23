import type React from "react";
import { interpolate, useCurrentFrame } from "remotion";
import type { ClaudeCodeData } from "../data/types";
import { clamp, colors } from "../theme";
import { type Span, SpanText, terminalText } from "./Terminal";

/** The colours Claude Code 2.1.309 printed in the captured session (24-bit SGR). */
export const claudeColors = {
  clawd: "#d77757",
  shimmer: "#eb9f7f",
  dim: "#999999",
  rule: "#888888",
  band: "#373737",
  white: "#ffffff",
  code: "#b1b9f9",
  mode: "#ffc107",
} as const;

/** Quadrants filled by each block element: upper left, upper right, lower left, lower right. */
const QUADRANTS: Record<string, readonly [boolean, boolean, boolean, boolean]> = {
  "█": [true, true, true, true],
  "▀": [true, true, false, false],
  "▐": [false, true, false, true],
  "▛": [true, true, true, false],
  "▜": [true, true, false, true],
  "▝": [false, true, false, false],
};

/** The mascot as Claude Code drew it: three rows of block elements; `bg` cells are black. */
const CLAWD: readonly { readonly text: string; readonly bg: readonly [number, number] }[] = [
  { text: " ▐▛███▛█", bg: [2, 7] },
  { text: "▝▜██████▀", bg: [2, 6] },
  { text: " ▝▝   ▝▝ ", bg: [-1, -1] },
];

/** Block elements drawn as rectangles on the character grid, so they join like in a terminal. */
const Clawd: React.FC<{ readonly cellWidth: number; readonly cellHeight: number; readonly top: number; readonly left: number }> = ({
  cellWidth,
  cellHeight,
  top,
  left,
}) => {
  // Each colour is one path of half-cell squares: within one path, squares that touch leave no
  // seam, whatever the scale.
  const half = (x: number, y: number) => `M${x * (cellWidth / 2)} ${y * (cellHeight / 2)}h${cellWidth / 2}v${cellHeight / 2}h${-cellWidth / 2}z`;
  let orange = "";
  let black = "";
  CLAWD.forEach((row, y) => {
    Array.from(row.text).forEach((char, x) => {
      const quadrants = QUADRANTS[char] ?? [false, false, false, false];
      quadrants.forEach((filled, q) => {
        const hx = 2 * x + (q % 2);
        const hy = 2 * y + Math.floor(q / 2);
        if (filled) orange += half(hx, hy);
        else if (x >= row.bg[0] && x <= row.bg[1]) black += half(hx, hy);
      });
    });
  });
  return (
    <svg style={{ position: "absolute", top, left }} width={9 * cellWidth} height={3 * cellHeight}>
      <path d={black} fill="#000000" />
      <path d={orange} fill={claudeColors.clawd} />
    </svg>
  );
};

/**
 * The spinner's frames, as Claude Code cycles them (· ✢ ✳ ✶ ✻ ✽). JetBrains Mono has only some
 * of these, so all are drawn: a dot, or a star of `spokes` arms.
 */
const SPINNER = [
  { spokes: 0, width: 0 },
  { spokes: 4, width: 0.13 },
  { spokes: 8, width: 0.09 },
  { spokes: 6, width: 0.1 },
  { spokes: 6, width: 0.14 },
  { spokes: 8, width: 0.13 },
] as const;

export const SpinnerGlyph: React.FC<{ readonly index: number; readonly size: number; readonly color: string }> = ({ index, size, color }) => {
  const { spokes, width } = SPINNER[index % SPINNER.length];
  return (
    <svg width={0.6 * size} height={size} viewBox="-0.3 -0.5 0.6 1" style={{ verticalAlign: "middle", marginTop: -0.12 * size }}>
      {spokes === 0 ? <circle r={0.07} fill={color} /> : null}
      {Array.from({ length: spokes }, (_, i) => {
        const angle = (i / spokes) * Math.PI * 2;
        return (
          <line
            key={angle}
            x1={0}
            y1={0}
            x2={Math.sin(angle) * 0.26}
            y2={-Math.cos(angle) * 0.26}
            stroke={color}
            strokeWidth={width}
            strokeLinecap="round"
          />
        );
      })}
    </svg>
  );
};

/** ⎿, which the font does not have: the corner before a tool's result. */
const Hook: React.FC<{ readonly size: number }> = ({ size }) => (
  <svg width={0.6 * size} height={size} viewBox="0 0 6 10" style={{ verticalAlign: "middle", marginTop: -0.1 * size }}>
    <path d="M2 0.5 V6 H6" fill="none" stroke={claudeColors.dim} strokeWidth={0.8} />
  </svg>
);

/** ⏵⏵, which the font does not have: the two triangles before the permission mode. */
const ModeArrows: React.FC<{ readonly size: number }> = ({ size }) => (
  <svg width={1.2 * size} height={size} viewBox="0 0 12 10" style={{ verticalAlign: "middle", marginTop: -0.1 * size }}>
    <path d="M1.4 2.6 L5 5 L1.4 7.4 Z M7.4 2.6 L11 5 L7.4 7.4 Z" fill={claudeColors.mode} />
  </svg>
);

/** A word with Claude Code's shimmer: a lighter band that runs along it while it works. */
const Shimmer: React.FC<{ readonly text: string; readonly frame: number }> = ({ text, frame }) => {
  const chars = Array.from(text);
  const at = Math.floor(frame / 2) % (chars.length + 8);
  return (
    <span>
      {chars.map((char, i) => (
        <span key={i} style={{ color: Math.abs(i - at) <= 1 ? claudeColors.shimmer : claudeColors.clawd }}>
          {char}
        </span>
      ))}
    </span>
  );
};

/** When the screen changes, in frames of the enclosing sequence. */
export type ClaudeTiming = {
  /** First character of the question typed into the input. */
  readonly typeStart: number;
  /** Enter: the question moves up and the work starts. */
  readonly enter: number;
  /** The model's first words (only when the run had them). */
  readonly preface: number;
  readonly reading: number;
  /** "Thought for …, read 1 file" takes the place of the reading lines. */
  readonly summary: number;
  readonly answer: number;
};

/** Frame at which each character of a typed text appears (a space takes two frames). */
export const typedCount = (text: string, start: number, frame: number) => {
  let at = start;
  let count = 0;
  for (const char of text) {
    if (at > frame) break;
    count++;
    at += char === " " ? 2 : 1;
  }
  return count;
};

type ClaudeCodeScreenProps = {
  readonly data: ClaudeCodeData;
  readonly timing: ClaudeTiming;
  readonly cols: number;
  readonly rows: number;
  readonly fontSize: number;
  readonly lineHeight: number;
  readonly frozenAt?: number;
  /** A thin outline in the accent colour around the answer, once it is there (not Claude Code's). */
  readonly highlightAnswer?: boolean;
};

type Row = { readonly row: number; readonly node: React.ReactNode; readonly band?: boolean };

/**
 * Claude Code's full-screen interface as captured: the header with the mascot, the conversation
 * from row 6, the spinner above the input box, the input between two rules and the mode line.
 */
export const ClaudeCodeScreen: React.FC<ClaudeCodeScreenProps> = ({ data, timing, cols, rows, fontSize, lineHeight, frozenAt, highlightAnswer = false }) => {
  const currentFrame = useCurrentFrame();
  const frame = frozenAt ?? currentFrame;
  const cellWidth = 0.6 * fontSize;
  const sent = frame >= timing.enter;
  const answered = frame >= timing.answer;
  const dim = (text: string, bold = false): Span => ({ text, color: claudeColors.dim, bold });

  const lines: Row[] = [];
  const put = (row: number, node: React.ReactNode, band = false) => lines.push({ row, node, band });

  // Header: mascot in columns 0–8, the text from column 11, the version from column 23.
  put(
    1,
    <>
      {" ".repeat(11)}
      <span style={{ fontWeight: 700 }}>{data.header.title}</span>
      {" ".repeat(Math.max(1, 12 - data.header.title.length))}
      <SpanText span={dim(data.header.version)} />
    </>,
  );
  put(2, <>{" ".repeat(11)}<SpanText span={dim(data.header.model)} /></>);
  put(3, <>{" ".repeat(11)}<SpanText span={dim(data.header.cwd)} /></>);

  // The conversation: blocks from row 8, one empty row between them.
  let next = 8;
  let answerRow = -1;
  const block = (height: number, node: React.ReactNode) => {
    put(next, node);
    next += height + 1;
  };
  if (sent) {
    put(
      6,
      <>
        <SpanText span={dim("❯ ")} />
        <SpanText span={{ text: data.question, color: claudeColors.white }} />
      </>,
      true,
    );
    if (data.preface && frame >= timing.preface) {
      block(
        1,
        <>
          <SpanText span={{ text: "●", color: claudeColors.white }} /> {data.preface}
        </>,
      );
    }
    if (frame >= timing.summary) {
      block(1, <>{"  "}{data.reading.summary.map((span, i) => <SpanText key={i} span={span} />)}</>);
    } else if (frame >= timing.reading) {
      const blink = Math.floor((frame - timing.reading) / 8) % 2 === 0;
      block(
        2,
        <>
          <div>
            <SpanText span={{ text: blink ? "●" : " ", color: claudeColors.dim }} /> {data.reading.title}
          </div>
          <div>
            {"  "}
            <Hook size={fontSize} />
            {"  "}
            <SpanText span={dim(data.reading.file)} />
          </div>
        </>,
      );
    }
    if (answered) {
      answerRow = next;
      block(
        1,
        <>
          <SpanText span={{ text: "●", color: claudeColors.white }} /> {data.answer.map((span, i) => <SpanText key={i} span={span} />)}
        </>,
      );
      put(
        next,
        <>
          <SpinnerGlyph index={4} size={fontSize} color={claudeColors.dim} /> <SpanText span={dim(data.done)} />
        </>,
      );
    } else if (frame >= timing.summary) {
      put(next, <SpanText span={{ text: "●", color: claudeColors.white }} />);
    }
    if (!answered) {
      const phase = Math.floor(interpolate(frame, [timing.enter, timing.answer], [0, data.working.length - 0.001], clamp));
      const { word, status } = data.working[phase];
      put(
        rows - 6,
        <>
          <SpinnerGlyph index={Math.floor((frame - timing.enter) / 3)} size={fontSize} color={claudeColors.clawd} /> <Shimmer text={word} frame={frame} />
          {status ? (
            <>
              {" "}
              <SpanText span={dim(status)} />
            </>
          ) : null}
        </>,
      );
    }
  } else {
    put(rows - 5, <>{" ".repeat(Math.max(0, cols - 2 - Array.from(data.effort).length))}<SpanText span={dim(data.effort)} /></>);
  }

  // The input box and the mode line.
  const rule = "─".repeat(cols);
  const typed = sent ? "" : data.question.slice(0, typedCount(data.question, timing.typeStart, frame));
  put(rows - 4, <SpanText span={{ text: rule, color: claudeColors.rule }} />);
  put(
    rows - 3,
    <>
      <SpanText span={sent && !answered ? dim("❯ ") : { text: "❯ " }} />
      {typed}
      <span style={{ backgroundColor: colors.terminalText }}> </span>
    </>,
  );
  put(rows - 2, <SpanText span={{ text: rule, color: claudeColors.rule }} />);
  const mode = data.footer.mode.replace(/^⏵⏵ /, "");
  put(
    rows - 1,
    <>
      {"  "}
      <ModeArrows size={fontSize} /> <SpanText span={{ text: mode, color: claudeColors.mode }} />
      <SpanText span={dim(answered ? data.footer.after : sent ? data.footer.busy : frame >= timing.typeStart ? data.footer.after : data.footer.idle)} />
    </>,
  );

  return (
    <div style={{ position: "relative", height: rows * lineHeight, ...terminalText(fontSize, lineHeight) }}>
      <Clawd cellWidth={cellWidth} cellHeight={lineHeight} top={lineHeight} left={0} />
      {highlightAnswer && answerRow >= 0 ? (
        <div
          style={{
            position: "absolute",
            top: answerRow * lineHeight - 0.2 * fontSize,
            left: -0.4 * fontSize,
            width: (2 + data.answer.reduce((n, span) => n + Array.from(span.text).length, 0)) * cellWidth + 0.8 * fontSize,
            height: lineHeight + 0.4 * fontSize,
            borderRadius: 0.3 * fontSize,
            border: `2px solid ${colors.accent}`,
            backgroundColor: "rgba(47, 129, 247, 0.08)",
            opacity: interpolate(frame, [timing.answer + 8, timing.answer + 18], [0, 1], clamp),
          }}
        />
      ) : null}
      {lines.map(({ row, node, band }) => (
        <div
          key={row}
          style={{
            position: "absolute",
            top: row * lineHeight,
            left: 0,
            width: cols * cellWidth,
            height: lineHeight,
            backgroundColor: band ? claudeColors.band : undefined,
          }}
        >
          {node}
        </div>
      ))}
    </div>
  );
};
