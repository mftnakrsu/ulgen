import { Interactive } from "remotion";
import { TerminalSession } from "../components/Terminal";
import { demoData } from "../data";
import { copy, type Lang } from "../i18n";
import { WINDOW } from "./layout";
import { loginSteps } from "./sessions";
import { TerminalScene } from "./TerminalScene";

const LoginSceneInner = ({ lang }: { readonly lang: Lang }) => (
  <TerminalScene command={copy[lang].login.command} title={copy[lang].login.title}>
    <TerminalSession steps={loginSteps(demoData[lang])} rows={WINDOW.rows} cols={WINDOW.cols} fontSize={WINDOW.fontSize} lineHeight={WINDOW.lineHeight} />
  </TerminalScene>
);

export const LoginScene = Interactive.create({ Component: LoginSceneInner, layout: "absolute-fill" });
