import type { Lang } from "./types";
import { t } from "./i18n";

const fa = "۰۱۲۳۴۵۶۷۸۹";

/** Persian prose uses Persian digits; everything else keeps Latin. */
export function digits(s: string | number, lang: Lang): string {
  const v = String(s);
  return lang === "fa" ? v.replace(/\d/g, (d) => fa[Number(d)] ?? d) : v;
}

const units = ["unit.b", "unit.kb", "unit.mb", "unit.gb", "unit.tb"] as const;

export function formatBytes(n: number, lang: Lang): string {
  let v = Math.max(0, n);
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  const num = i === 0 || v >= 100 ? Math.round(v).toString() : v.toFixed(v >= 10 ? 0 : 1);
  return `${digits(num, lang)} ${t(lang, units[i] ?? "unit.b")}`;
}

export function formatRate(bytesPerSecond: number, lang: Lang): string {
  return `${formatBytes(bytesPerSecond, lang)}${t(lang, "unit.perSecond")}`;
}

export function formatDuration(totalSeconds: number, lang: Lang): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const pad = (x: number) => x.toString().padStart(2, "0");
  const out = h > 0 ? `${h}:${pad(m)}:${pad(s % 60)}` : `${m}:${pad(s % 60)}`;
  return digits(out, lang);
}

export function formatLatency(ms: number, lang: Lang): string {
  return `${digits(ms, lang)} ${t(lang, "unit.ms")}`;
}

/** Keeps both ends of an invite visible: dotvpn://AbC…xYz. */
export function middleTruncate(s: string, max = 36): string {
  if (s.length <= max) return s;
  const keep = max - 1;
  const head = Math.ceil(keep * 0.6);
  return `${s.slice(0, head)}…${s.slice(s.length - (keep - head))}`;
}
