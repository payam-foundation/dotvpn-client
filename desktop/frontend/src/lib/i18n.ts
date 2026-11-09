import en from "../i18n/en.json";
import fa from "../i18n/fa.json";
import type { Lang } from "./types";

export type Key = keyof typeof en;

const dict: Record<Lang, Record<string, string>> = { en, fa };

export function t(lang: Lang, key: string, vars?: Record<string, string | number>): string {
  let s = dict[lang][key] ?? dict.en[key] ?? key;
  if (vars) for (const [k, v] of Object.entries(vars)) s = s.replace(`{${k}}`, String(v));
  return s;
}

export const dir = (lang: Lang): "ltr" | "rtl" => (lang === "fa" ? "rtl" : "ltr");

/** First launch: follow the system language, English unless it is Persian. */
export function systemLang(languages: readonly string[] = navigator.languages): Lang {
  return languages.some((l) => l.toLowerCase().startsWith("fa")) ? "fa" : "en";
}

/** The message shown for a failed call. Unknown codes fall back to the generic line. */
export function errorText(lang: Lang, code: string): string {
  const key = `err.${code}`;
  return key in en ? t(lang, key) : t(lang, "err.internal");
}
