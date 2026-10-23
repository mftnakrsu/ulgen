/**
 * The subtitle script of the demo, per language: what is said while each part plays. It is
 * burned into the captioned and voiced renders (components/Subtitles.tsx) and exported as .srt,
 * .vtt and .json by scripts/export-captions.mjs, so the voiceover is generated from the same lines.
 *
 * Times are in milliseconds from the start of the composition. Keep each line short (one line on
 * the 16:9 frame) and leave a small gap between cues.
 *
 * This file is also imported by Node directly (type stripping), so it may only use erasable
 * TypeScript: type-only imports, no enums.
 */
import type { Lang } from "./i18n";

export type Cue = {
  readonly startMs: number;
  readonly endMs: number;
  readonly text: string;
  /**
   * What the voiceover says instead of `text`, when the subtitle would be read wrong by the
   * text-to-speech voice (product names, web addresses, EVREN in capitals). Only the voiceover
   * uses it; the subtitles always show `text`. It is only a respelling: the voice says the same
   * words as the subtitle. A line too long to fit its cue when spoken is shortened in `text`
   * itself.
   */
  readonly say?: string;
};

export type CaptionScript = {
  /** Length of the composition the cues belong to. */
  readonly durationMs: number;
  /** Output file name without extension and language, e.g. ulgen-demo → ulgen-demo-en.srt. */
  readonly file: string;
  readonly cues: Record<Lang, readonly Cue[]>;
};

/**
 * UlgenDemo, 40.5 s. Scenes (with their 0.5 s fades): Opening 0–3.5, Login 3–9.5, Setup 9–16,
 * Health 15.5–23, Claude Code 22.5–32.5, Key 32–36.5, Outro 36–40.5.
 */
const demo: CaptionScript = {
  durationMs: 40500,
  file: "ulgen-demo",
  cues: {
    en: [
      { startMs: 300, endMs: 3400, text: "Ülgen: EVREN for the coding tools you already use.", say: "Ulgen: Evren for the coding tools you already use." },
      { startMs: 3700, endMs: 6200, text: "Sign in once; the key stays hidden." },
      { startMs: 6400, endMs: 9200, text: "Accept the terms; the key goes to the keychain." },
      { startMs: 9500, endMs: 12600, text: "Setup shows the change before it writes anything." },
      { startMs: 12800, endMs: 15600, text: "Then it writes, and keeps a backup." },
      { startMs: 16000, endMs: 19200, text: "Doctor checks the key, EVREN and every tool.", say: "Doctor checks the key, Evren and every tool." },
      { startMs: 19400, endMs: 22600, text: "Status shows today's quota and the models." },
      { startMs: 23000, endMs: 26100, text: "ulgen claude starts Claude Code on an EVREN model.", say: "Ulgen Claude starts Claude Code on an Evren model." },
      { startMs: 26400, endMs: 29100, text: "It reads the file and answers." },
      { startMs: 29400, endMs: 32200, text: "Claude Code never sees your EVREN key.", say: "Claude Code never sees your Evren key." },
      { startMs: 32500, endMs: 35900, text: "Your key stays yours: never printed, never logged." },
      { startMs: 36300, endMs: 40200, text: "Install it in one line. Docs at ulgen.mefby.com.", say: "Install it in one line. Docs at ulgen dot mefby dot com." },
    ],
    tr: [
      { startMs: 300, endMs: 3400, text: "Ülgen: kodlama araçlarınız için EVREN.", say: "Ülgen: kodlama araçlarınız için Evren." },
      { startMs: 3700, endMs: 6200, text: "Bir kez giriş yapın; anahtar gizli kalır." },
      { startMs: 6400, endMs: 9200, text: "Koşulları kabul edin; anahtar kaydedilir." },
      { startMs: 9500, endMs: 12600, text: "Kurulum, yazmadan önce değişikliği gösterir." },
      { startMs: 12800, endMs: 15600, text: "Sonra yazar ve yedeğini alır." },
      { startMs: 16000, endMs: 19200, text: "Doctor; anahtarı, EVREN'i ve araçları denetler.", say: "Doktır; anahtarı, Evren'i ve araçları denetler." },
      { startMs: 19400, endMs: 22600, text: "Status, günlük kotayı ve modelleri gösterir.", say: "Steytıs, günlük kotayı ve modelleri gösterir." },
      { startMs: 22800, endMs: 26400, text: "ulgen claude, Claude Code'u bir EVREN modeliyle başlatır.", say: "Ülgen klod, Klod Kod'u bir Evren modeliyle başlatır." },
      { startMs: 26600, endMs: 29100, text: "Dosyayı okur ve yanıtlar." },
      { startMs: 29400, endMs: 32200, text: "Claude Code, EVREN anahtarınızı hiç görmez.", say: "Klod Kod, Evren anahtarınızı hiç görmez." },
      { startMs: 32500, endMs: 35900, text: "Anahtarınız sizde kalır; ekrana ya da günlüğe yazılmaz." },
      { startMs: 36300, endMs: 40200, text: "Tek satırla kurun. Belgeler ulgen.mefby.com'da.", say: "Tek satırla kurun. Belgeler ülgen nokta mefbi nokta kom'da." },
    ],
  },
};

export const captionScripts = { demo } as const;
