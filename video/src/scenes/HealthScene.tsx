import { Interactive } from "remotion";
import { TerminalSession } from "../components/Terminal";
import { demoData } from "../data";
import { copy, type Lang } from "../i18n";
import { WINDOW } from "./layout";
import { healthSteps } from "./sessions";
import { TerminalScene } from "./TerminalScene";

const HealthSceneInner = ({ lang }: { readonly lang: Lang }) => (
  <TerminalScene command={copy[lang].health.command} title={copy[lang].health.title}>
    <TerminalSession steps={healthSteps(demoData[lang])} rows={WINDOW.rows} cols={WINDOW.cols} fontSize={WINDOW.fontSize} lineHeight={WINDOW.lineHeight} />
  </TerminalScene>
);

export const HealthScene = Interactive.create({ Component: HealthSceneInner, layout: "absolute-fill" });
