import type { Lang } from "../i18n";
import { data as en } from "./en";
import { data as tr } from "./tr";
import type { DemoData } from "./types";

/** The terminal text of each language's video. */
export const demoData: Record<Lang, DemoData> = { en, tr };
