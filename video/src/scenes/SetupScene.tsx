import { Interactive } from "remotion";
import { TerminalSession } from "../components/Terminal";
import { demoData } from "../data";
import { copy, type Lang } from "../i18n";
import { WINDOW } from "./layout";
import { setupSteps } from "./sessions";
import { TerminalScene } from "./TerminalScene";

const SetupSceneInner = ({ lang }: { readonly lang: Lang }) => (
  <TerminalScene command={copy[lang].setup.command} title={copy[lang].setup.title}>
    <TerminalSession steps={setupSteps(demoData[lang])} rows={WINDOW.rows} cols={WINDOW.cols} fontSize={WINDOW.fontSize} lineHeight={WINDOW.lineHeight} />
  </TerminalScene>
);

export const SetupScene = Interactive.create({ Component: SetupSceneInner, layout: "absolute-fill" });
