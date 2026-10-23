import type { ClaudeTiming } from "../components/ClaudeCode";
import { command, type Line, print, prompt, type Span, type Step, type, wait, write } from "../components/Terminal";
import type { DemoData } from "../data/types";

/**
 * The terminal sessions of the scenes, as steps (components/Terminal.tsx). Every line of output is
 * the captured one (src/data/); only the pace is the video's: output that took seconds to arrive
 * comes faster, as in the README recording.
 */

/** The terms of use follow the intro line in full; the video draws them as grey bars. */
const TERMS: readonly Line[] = [92, 101, 87, 64].map((width): readonly Span[] => [{ text: "x".repeat(width), bar: true }]);

/** `ulgen login`, in the order login.go prints it, for an account that has not accepted the terms yet. */
export const loginSteps = (data: DemoData): Step[] => [
  wait(10),
  ...command("ulgen login"),
  wait(6),
  print(data.login.openPage, 2),
  wait(8),
  write(data.login.keyPrompt),
  // The key is pasted here; the terminal shows nothing of it.
  wait(32),
  write("\n"),
  wait(10),
  print(["", data.login.termsIntro, ""], 1),
  print(TERMS, 1),
  print([""], 0),
  write(data.login.acceptPrompt),
  wait(12),
  type(data.login.yes),
  wait(8),
  print([data.login.accepted, data.login.saved], 4),
  wait(8),
  // The status report that ends a sign-in (writeStatusText), as `ulgen status` prints it.
  print(["", ...data.status.slice(0, 3)], 2),
];

/** `ulgen setup opencode --dry-run`, then the real `ulgen setup opencode` with its backup. */
export const setupSteps = (data: DemoData): Step[] => [
  wait(10),
  ...command("ulgen setup opencode --dry-run"),
  wait(8),
  // 128 lines: in a real terminal they come at once; here they scroll by in about a second.
  print(data.setupDryRun, 0.25),
  wait(32),
  ...command("ulgen setup opencode"),
  wait(8),
  print(data.setupApply, 3),
  wait(6),
  prompt(),
];

/** `ulgen doctor`, then `ulgen status`. */
export const healthSteps = (data: DemoData): Step[] => [
  wait(10),
  ...command("ulgen doctor"),
  wait(10),
  print(data.doctor, 2),
  wait(56),
  ...command("ulgen status"),
  wait(10),
  print(data.status, 2),
  wait(4),
  prompt(),
];

/** The shell before Claude Code takes over the screen: the command and ulgen's notice. */
export const claudeShellSteps = (data: DemoData): Step[] => [wait(8), ...command("ulgen claude"), wait(5), print([data.claude.notice], 0)];

/** Claude Code's screen appears at this frame of the scene. */
export const CLAUDE_UI_AT = 54;

export const claudeTiming: ClaudeTiming = {
  typeStart: 66,
  enter: 118,
  preface: 134,
  reading: 148,
  summary: 186,
  answer: 198,
};

/** The stills of the gallery: the end of a session, with the prompt back. */
export const doctorStill = (data: DemoData): Step[] => [...command("ulgen doctor"), print(data.doctor, 0), prompt()];
export const statusStill = (data: DemoData): Step[] => [...command("ulgen status"), print(data.status, 0), prompt()];
export const setupStill = (data: DemoData): Step[] => [
  ...command("ulgen setup"),
  print(data.setupList, 0),
  ...command("ulgen setup opencode"),
  print(data.setupApply, 0),
  prompt(),
];
