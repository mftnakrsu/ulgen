import type React from "react";
import { colors } from "../theme";

type LogoProps = {
  readonly size: number;
  /** 0–1: how much of the tree's lines is drawn. */
  readonly draw?: number;
  /** 0–1: how far the five dots have grown. */
  readonly dots?: number;
  readonly style?: React.CSSProperties;
};

const LINES = ["M128 220V150", "M92 220h72", "M128 180c0-32-46-20-69-43", "M128 180c0-32 46-20 69-43", "M128 150c0-34-36-30-50-64", "M128 150c0-34 36-30 50-64"];
const DOTS = [
  { cx: 128, cy: 55, r: 21 },
  { cx: 78, cy: 86, r: 16 },
  { cx: 178, cy: 86, r: 16 },
  { cx: 59, cy: 137, r: 16 },
  { cx: 197, cy: 137, r: 16 },
];

/** The Ülgen logo (docs/assets/logo.svg): a tree of five dots in the accent blue. */
export const Logo: React.FC<LogoProps> = ({ size, draw = 1, dots = 1, style }) => (
  <svg viewBox="0 0 256 256" width={size} height={size} style={style}>
    <g fill="none" stroke={colors.accent} strokeWidth={14} strokeLinecap="round" strokeLinejoin="round">
      {LINES.map((d) => (
        <path key={d} d={d} pathLength={1} strokeDasharray={1} strokeDashoffset={1 - draw} opacity={draw > 0 ? 1 : 0} />
      ))}
    </g>
    <g fill={colors.accent}>
      {DOTS.map((dot) => (
        <circle key={`${dot.cx}-${dot.cy}`} cx={dot.cx} cy={dot.cy} r={dot.r * dots} />
      ))}
    </g>
  </svg>
);
