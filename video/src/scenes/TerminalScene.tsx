import type React from "react";
import { Headline, Stage } from "../components/Stage";
import { TerminalWindow } from "../components/Terminal";
import { HEADLINE, WINDOW } from "./layout";

type TerminalSceneProps = {
  readonly command: string;
  readonly title: string;
  /** What the window shows. */
  readonly children: React.ReactNode;
};

/** A scene of the demo: the headline, and below it the terminal window. */
export const TerminalScene: React.FC<TerminalSceneProps> = ({ command, title, children }) => (
  <Stage>
    <Headline command={command} title={title} fontSize={HEADLINE.fontSize} style={{ left: HEADLINE.left, top: HEADLINE.top }} />
    <TerminalWindow width={WINDOW.width} height={WINDOW.height} fontSize={WINDOW.fontSize} style={{ position: "absolute", left: WINDOW.left, top: WINDOW.top }}>
      {children}
    </TerminalWindow>
  </Stage>
);
