import type { Line, Span } from "../components/Terminal";

/**
 * Claude Code's screen in the captured `ulgen claude` session, as text and colours (it prints
 * colours itself). Claude Code's own words are English in both languages; only the question,
 * the answer and the times differ.
 */
export type ClaudeCodeData = {
  /** The line ulgen prints before it starts Claude Code. */
  readonly notice: string;
  readonly header: { readonly title: string; readonly version: string; readonly model: string; readonly cwd: string };
  /** Bottom right of the empty screen. */
  readonly effort: string;
  readonly question: string;
  /** What the model wrote before it read the file (the English run did, the Turkish one did not). */
  readonly preface?: string;
  readonly reading: { readonly title: string; readonly file: string; readonly summary: readonly Span[] };
  /** The spinner line while it works, in the order seen: the word and the counters after it. */
  readonly working: readonly { readonly word: string; readonly status: string }[];
  readonly answer: readonly Span[];
  readonly done: string;
  readonly footer: { readonly mode: string; readonly idle: string; readonly busy: string; readonly after: string };
};

/** Everything the terminal shows in one language. */
export type DemoData = {
  /** `ulgen login`, put together from cmd/ulgen/login.go and the message catalog (see README). */
  readonly login: {
    readonly openPage: readonly Line[];
    readonly keyPrompt: string;
    readonly termsIntro: string;
    readonly acceptPrompt: string;
    readonly yes: string;
    readonly accepted: string;
    readonly saved: string;
  };
  readonly setupList: readonly Line[];
  readonly setupDryRun: readonly Line[];
  readonly setupApply: readonly Line[];
  readonly doctor: readonly Line[];
  readonly status: readonly Line[];
  readonly claude: ClaudeCodeData;
};
