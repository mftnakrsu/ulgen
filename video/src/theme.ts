import { loadFont } from "@remotion/fonts";
import { staticFile } from "remotion";

/**
 * The video's colours. The terminal is the "GitHub Dark" theme of the README recording
 * (docs/assets/demo.tape, made with vhs); the accent is the logo's blue (docs/assets/logo.svg).
 */
export const colors = {
  /** Behind the terminal window. */
  page: "#0b0e13",
  /** A faint glow of the accent behind the window. */
  glow: "rgba(47, 129, 247, 0.16)",
  /** Headlines and the big type of the opening and the outro. */
  ink: "#e6edf3",
  muted: "#8b949e",
  accent: "#2F81F7",
  /** The terminal: background, text, the window's title bar and border. */
  terminal: "#101216",
  terminalText: "#c9d1d9",
  terminalDim: "#6e7681",
  titleBar: "#1a1d23",
  border: "#30363d",
  cursor: "#c9d1d9",
} as const;

/** The interface font: headlines, captions and subtitles. */
export const fontFamily = "Atkinson Hyperlegible Next";
/** The terminal font, as in the recording vhs makes (JetBrains Mono is its default). */
export const monoFamily = "JetBrains Mono";

// Atkinson Hyperlegible Next in its two subsets: Latin, and Latin Extended for ğ, ş and İ.
loadFont({
  family: fontFamily,
  url: staticFile("fonts/atkinson.woff2"),
  weight: "200 800",
  unicodeRange:
    "U+0000-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,U+0304,U+0308,U+0329,U+2000-206F,U+20AC,U+2122,U+2191,U+2193,U+2212,U+2215,U+FEFF,U+FFFD",
});
loadFont({
  family: fontFamily,
  url: staticFile("fonts/atkinson-ext.woff2"),
  weight: "200 800",
  unicodeRange:
    "U+0100-02BA,U+02BD-02C5,U+02C7-02CC,U+02CE-02D7,U+02DD-02FF,U+0304,U+0308,U+0329,U+1D00-1DBF,U+1E00-1E9F,U+1EF2-1EFF,U+2020,U+20A0-20AB,U+20AD-20C0,U+2113,U+2C60-2C7F,U+A720-A7FF",
});
// JetBrains Mono 2.304, the whole variable font: Turkish letters, box drawing and block elements.
loadFont({ family: monoFamily, url: staticFile("fonts/jetbrains-mono.woff2"), weight: "100 800" });

/** Shared extrapolation for interpolate() calls that should hold their end values. */
export const clamp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;
