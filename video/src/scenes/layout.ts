import { rowsFor, TERMINAL_PADDING } from "../components/Terminal";

/**
 * The terminal window of the demo's scenes, on 1920×1080: 108 columns by 20 rows, the size the
 * real output was captured at (the Claude Code screen is laid out for exactly this size).
 */
const fontSize = 25;
const lineHeight = 36;
const width = 1668;
const height = 842;

export const WINDOW = {
  left: (1920 - width) / 2,
  top: 168,
  width,
  height,
  fontSize,
  lineHeight,
  cols: Math.floor((width - 2 - 2 * TERMINAL_PADDING * fontSize) / (0.6 * fontSize)),
  rows: rowsFor(height - 2, fontSize, lineHeight),
} as const;

/** The headline above the window. */
export const HEADLINE = { left: WINDOW.left, top: 58, fontSize: 52 } as const;
