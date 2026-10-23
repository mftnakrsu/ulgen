import type React from "react";
import { useMemo } from "react";
import { useCurrentFrame } from "remotion";
import { colors, monoFamily } from "../theme";

/** A run of text in one style. Colours are used only where the real program prints them. */
export type Span = {
  readonly text: string;
  readonly color?: string;
  readonly bold?: boolean;
  /**
   * Drawn as a grey bar as wide as the text instead of the text: for words the video may not
   * show (the terms of use are long, Turkish, and not reproduced here).
   */
  readonly bar?: boolean;
};
/** One line of output: plain text, or styled runs. */
export type Line = string | readonly Span[];

/**
 * One step of a terminal session. A session is played like a real shell: text is written at the
 * cursor, which is always at the end of what was written; "\n" starts a new line.
 */
export type Step =
  /** Typed at the cursor, one character after the other; `enter` (default true) ends the line. */
  | { readonly kind: "type"; readonly text: string; readonly framesPerChar: number; readonly enter: boolean }
  /** Lines a program prints, one after the other, `framesPerLine` apart (0: all at once). */
  | { readonly kind: "print"; readonly lines: readonly Line[]; readonly framesPerLine: number }
  /** Text a program writes without ending the line, such as a question waiting for an answer. */
  | { readonly kind: "write"; readonly line: Line }
  | { readonly kind: "wait"; readonly frames: number }
  | { readonly kind: "clear" };

export const PROMPT = "$ ";

export const type = (text: string, options: { framesPerChar?: number; enter?: boolean } = {}): Step => ({
  kind: "type",
  text,
  framesPerChar: options.framesPerChar ?? 1,
  enter: options.enter ?? true,
});
export const print = (lines: readonly Line[], framesPerLine = 1): Step => ({ kind: "print", lines, framesPerLine });
export const write = (line: Line): Step => ({ kind: "write", line });
export const wait = (frames: number): Step => ({ kind: "wait", frames });
export const clear = (): Step => ({ kind: "clear" });
/** The shell's prompt (PS1='$ ', as in the README recording). */
export const prompt = (): Step => write(PROMPT);
/** A command typed at the prompt and run: the prompt, the typing, Enter. */
export const command = (text: string, options: { framesPerChar?: number } = {}): Step[] => [prompt(), type(text, options)];

type Event = { readonly at: number; readonly spans: readonly Span[] | "clear"; readonly typed: boolean };

const toSpans = (line: Line): readonly Span[] => (typeof line === "string" ? [{ text: line }] : line);

/** Turns the steps into timed writes; also returns the frame at which the session is done. */
export const compile = (steps: readonly Step[], start = 0) => {
  const events: Event[] = [];
  let at = start;
  for (const step of steps) {
    switch (step.kind) {
      case "type":
        for (const char of step.text) {
          events.push({ at, spans: [{ text: char }], typed: true });
          // A space takes a little longer, like a person typing.
          at += char === " " ? step.framesPerChar * 2 : step.framesPerChar;
        }
        if (step.enter) {
          at += 4;
          events.push({ at, spans: [{ text: "\n" }], typed: true });
          at += 3;
        }
        break;
      case "print":
        for (const line of step.lines) {
          events.push({ at, spans: [...toSpans(line), { text: "\n" }], typed: false });
          at += step.framesPerLine;
        }
        break;
      case "write":
        events.push({ at, spans: toSpans(step.line), typed: false });
        break;
      case "wait":
        at += step.frames;
        break;
      case "clear":
        events.push({ at, spans: "clear", typed: false });
        break;
    }
  }
  return { events, end: at };
};

type Screen = { lines: Span[][]; lastWrite: number; typing: boolean };

/** The screen after every write up to `frame`. */
const replay = (events: readonly Event[], frame: number): Screen => {
  let lines: Span[][] = [[]];
  let lastWrite = -Infinity;
  let typing = false;
  for (const event of events) {
    if (event.at > frame) break;
    lastWrite = event.at;
    typing = event.typed;
    if (event.spans === "clear") {
      lines = [[]];
      continue;
    }
    for (const span of event.spans) {
      const parts = span.text.split("\n");
      parts.forEach((part, index) => {
        if (index > 0) lines.push([]);
        if (part !== "") lines[lines.length - 1].push({ ...span, text: part });
      });
    }
  }
  return { lines, lastWrite, typing };
};

/** The cursor blinks once a second, and stays on while someone types. */
export const cursorVisible = (frame: number, lastWrite: number, typing: boolean) =>
  (typing && frame - lastWrite < 6) || Math.floor((frame - lastWrite) / 15) % 2 === 0;

export const Cursor: React.FC<{ readonly visible: boolean; readonly fontSize: number }> = ({ visible, fontSize }) => (
  <span
    style={{
      display: "inline-block",
      width: "0.6em",
      height: fontSize * 1.2,
      verticalAlign: "text-bottom",
      backgroundColor: visible ? colors.cursor : "transparent",
    }}
  />
);

export const SpanText: React.FC<{ readonly span: Span }> = ({ span }) =>
  span.bar ? (
    <span
      style={{
        display: "inline-block",
        width: `${0.6 * Array.from(span.text).length}em`,
        height: "0.36em",
        verticalAlign: "middle",
        borderRadius: "0.18em",
        backgroundColor: colors.border,
      }}
    />
  ) : (
    <span style={{ color: span.color, fontWeight: span.bold ? 700 : undefined }}>{span.text}</span>
  );

/** Monospace text as the terminal shows it: no ligatures, no wrapping. */
export const terminalText = (fontSize: number, lineHeight: number): React.CSSProperties => ({
  fontFamily: monoFamily,
  fontSize,
  lineHeight: `${lineHeight}px`,
  fontVariantLigatures: "none",
  fontFeatureSettings: '"liga" 0, "calt" 0',
  whiteSpace: "pre",
  color: colors.terminalText,
});

type TerminalSessionProps = {
  readonly steps: readonly Step[];
  /** How many lines fit; older lines scroll off the top. */
  readonly rows: number;
  readonly fontSize: number;
  readonly lineHeight: number;
  /** Frame (of the enclosing sequence) at which the session starts. */
  readonly start?: number;
  readonly showCursor?: boolean;
  /** Width of the screen in characters; longer lines wrap, as in a real terminal. */
  readonly cols: number;
  /** Show the screen as it is at this frame, whatever the current frame (for stills). */
  readonly frozenAt?: number;
};

/** Splits a line into screen rows of at most `cols` characters. */
const wrap = (line: readonly Span[], cols: number): Span[][] => {
  const rows: Span[][] = [[]];
  let used = 0;
  for (const span of line) {
    let rest = Array.from(span.text);
    while (rest.length > 0) {
      if (used === cols) {
        rows.push([]);
        used = 0;
      }
      const take = rest.slice(0, cols - used);
      rows[rows.length - 1].push({ ...span, text: take.join("") });
      used += take.length;
      rest = rest.slice(take.length);
    }
  }
  return rows;
};

/** A shell session played back: commands typed, output printed line by line, a blinking cursor. */
export const TerminalSession: React.FC<TerminalSessionProps> = ({ steps, rows, cols, fontSize, lineHeight, start = 0, showCursor = true, frozenAt }) => {
  const currentFrame = useCurrentFrame();
  const frame = frozenAt ?? currentFrame;
  const { events } = useMemo(() => compile(steps, start), [steps, start]);
  const screen = replay(events, frame);
  const visible = screen.lines.flatMap((line) => wrap(line, cols)).slice(-rows);
  return (
    <div style={terminalText(fontSize, lineHeight)}>
      {visible.map((line, row) => (
        // Rows are positions on the screen, so the index is the right key.
        <div key={row} style={{ height: lineHeight }}>
          {line.map((span, index) => (
            <SpanText key={index} span={span} />
          ))}
          {showCursor && row === visible.length - 1 ? (
            <Cursor visible={frozenAt !== undefined || cursorVisible(frame, screen.lastWrite, screen.typing)} fontSize={fontSize} />
          ) : null}
        </div>
      ))}
    </div>
  );
};

type TerminalWindowProps = {
  readonly width: number;
  readonly height: number;
  /** Font size of the terminal text; the title bar and padding follow it. */
  readonly fontSize: number;
  readonly children: React.ReactNode;
  readonly style?: React.CSSProperties;
};

/** Padding inside the window around the text, in units of the font size. */
export const TERMINAL_PADDING = 0.9;
/** Height of the title bar, in units of the font size. */
export const TITLE_BAR = 1.7;

/** How many lines of `lineHeight` fit in a window of this height. */
export const rowsFor = (height: number, fontSize: number, lineHeight: number) =>
  Math.floor((height - TITLE_BAR * fontSize - 2 * TERMINAL_PADDING * fontSize) / lineHeight);

/** A terminal window in the GitHub Dark theme of the README recording. */
export const TerminalWindow: React.FC<TerminalWindowProps> = ({ width, height, fontSize, children, style }) => (
  <div
    style={{
      width,
      height,
      borderRadius: fontSize * 0.5,
      overflow: "hidden",
      backgroundColor: colors.terminal,
      border: `1px solid ${colors.border}`,
      boxShadow: "0 30px 80px rgba(0, 0, 0, 0.55)",
      display: "flex",
      flexDirection: "column",
      ...style,
    }}
  >
    <div
      style={{
        height: TITLE_BAR * fontSize,
        flexShrink: 0,
        display: "flex",
        alignItems: "center",
        gap: fontSize * 0.36,
        paddingLeft: fontSize * 0.7,
        backgroundColor: colors.titleBar,
        borderBottom: `1px solid ${colors.border}`,
      }}
    >
      {["#f85149", "#d29922", "#3fb950"].map((dot) => (
        <div key={dot} style={{ width: fontSize * 0.46, height: fontSize * 0.46, borderRadius: "50%", backgroundColor: dot, opacity: 0.85 }} />
      ))}
    </div>
    <div style={{ position: "relative", flex: 1, padding: TERMINAL_PADDING * fontSize, overflow: "hidden" }}>{children}</div>
  </div>
);
