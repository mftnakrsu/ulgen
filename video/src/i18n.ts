/**
 * Every word the video itself puts on screen, per language. What the terminal shows is program
 * output and lives in src/data/ instead. Wording that also appears in the README matches it
 * (README.md, README.tr.md).
 */
export type Lang = "en" | "tr";

type Scene = {
  /** The command the scene is about, shown before the headline. */
  readonly command: string;
  readonly title: string;
};

type Copy = {
  /** The tagline of the README, under the name. */
  readonly opening: { readonly tagline: string };
  readonly login: Scene;
  readonly setup: Scene;
  readonly health: Scene;
  readonly claude: Scene;
  /** Three lines of the README's "Your key stays yours". */
  readonly key: { readonly title: string; readonly points: readonly [string, string, string] };
  readonly outro: { readonly install: string; readonly docs: string; readonly code: string };
};

export const copy: Record<Lang, Copy> = {
  en: {
    opening: { tagline: "EVREN for the coding tools you already use" },
    login: { command: "ulgen login", title: "Sign in once" },
    setup: { command: "ulgen setup", title: "See the change before it is written" },
    health: { command: "ulgen doctor · ulgen status", title: "One look at everything" },
    claude: { command: "ulgen claude", title: "Claude Code on an EVREN model" },
    key: {
      title: "Your key stays yours",
      points: [
        "Stored in the operating system's credential store",
        "Never written into a tool's settings file",
        "Claude Code never receives it at all",
      ],
    },
    outro: { install: "Install", docs: "Docs", code: "Code" },
  },
  tr: {
    opening: { tagline: "Zaten kullandığınız kodlama araçları için EVREN" },
    login: { command: "ulgen login", title: "Bir kez giriş yapın" },
    setup: { command: "ulgen setup", title: "Değişikliği yazılmadan önce görün" },
    health: { command: "ulgen doctor · ulgen status", title: "Her şeye tek bakış" },
    claude: { command: "ulgen claude", title: "Bir EVREN modeliyle Claude Code" },
    key: {
      title: "Anahtarınız sizde kalır",
      points: ["İşletim sisteminin kimlik deposunda saklanır", "Hiçbir aracın ayar dosyasına yazılmaz", "Claude Code anahtarı hiç almaz"],
    },
    outro: { install: "Kurulum", docs: "Belgeler", code: "Kod" },
  },
};
