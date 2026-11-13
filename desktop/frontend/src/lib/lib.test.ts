import { describe, expect, it } from "vitest";
import { digits, formatBytes, formatDuration, formatLatency, formatRate, middleTruncate } from "./format";
import en from "../i18n/en.json";
import fa from "../i18n/fa.json";
import { flagCode, flagEmoji } from "./flag";
import { dir, errorText, systemLang, t } from "./i18n";
import { groupNodes } from "./nodes";
import { MAX_SAMPLES, push, rate } from "./throughput";
import { isCIDR, isDomain, parseEntries, parseEntry } from "./validate";

describe("format", () => {
  it("uses Persian digits only for fa", () => {
    expect(digits(42, "fa")).toBe("۴۲");
    expect(digits(42, "en")).toBe("42");
    expect(formatLatency(42, "fa")).toBe("۴۲ میلی‌ثانیه");
    expect(formatLatency(42, "en")).toBe("42 ms");
  });

  it("scales bytes", () => {
    expect(formatBytes(0, "en")).toBe("0 B");
    expect(formatBytes(1536, "en")).toBe("1.5 KB");
    expect(formatBytes(48_000_000, "en")).toBe("46 MB");
    expect(formatBytes(3 * 1024 ** 3, "en")).toBe("3.0 GB");
    expect(formatBytes(-5, "en")).toBe("0 B");
    expect(formatRate(2048, "en")).toBe("2.0 KB/s");
  });

  it("formats uptime", () => {
    expect(formatDuration(5, "en")).toBe("0:05");
    expect(formatDuration(754, "en")).toBe("12:34");
    expect(formatDuration(3725, "en")).toBe("1:02:05");
    expect(formatDuration(754, "fa")).toBe("۱۲:۳۴");
    expect(formatDuration(-3, "en")).toBe("0:00");
  });

  it("keeps both ends of a long invite", () => {
    const s = "dv://" + "a".repeat(80) + "END";
    const out = middleTruncate(s, 30);
    expect(out).toHaveLength(30);
    expect(out.startsWith("dv://")).toBe(true);
    expect(out.endsWith("END")).toBe(true);
    expect(middleTruncate("short", 30)).toBe("short");
  });
});

describe("i18n", () => {
  it("has the same keys in en and fa", () => {
    expect(Object.keys(fa).sort()).toEqual(Object.keys(en).sort());
  });

  it("uses ZWNJ and Persian punctuation, never ASCII comma in fa prose", () => {
    for (const [k, v] of Object.entries(fa)) {
      if (/[\u0600-\u06FF]/.test(v)) expect(v, k).not.toMatch(/[\u0600-\u06FF],/);
    }
  });

  it("keeps placeholders in both languages", () => {
    expect(fa["servers.cooldown"]).toContain("{s}");
    expect(t("fa", "servers.cooldown", { s: "۳" })).toContain("۳");
  });

  it("follows the guidelines' copy", () => {
    expect(en["about.limits"]).toBe("Dot VPN hides that you use a VPN. It does not hide that you use the internet.");
    expect(fa["err.bad_invite"]).toBe("این لینک دعوت معتبر نیست. از مدیر سرور لینک تازه بخواهید.");
    expect(en["err.fallback"]).toBe("Couldn't reach the server. Trying the next route.");
  });

  it("bans the banned words and exclamation marks", () => {
    const banned = /unblockable|unstoppable|military-grade|100% anonymous|bulletproof|uncensorable|!|—/i;
    for (const [k, v] of Object.entries(en)) expect(v, k).not.toMatch(banned);
  });

  it("picks direction and system language", () => {
    expect(dir("fa")).toBe("rtl");
    expect(dir("en")).toBe("ltr");
    expect(systemLang(["fa-IR", "en"])).toBe("fa");
    expect(systemLang(["de-DE", "en"])).toBe("en");
  });

  it("falls back to the generic error for unknown codes", () => {
    expect(errorText("en", "bad_invite")).toBe(en["err.bad_invite"]);
    expect(errorText("en", "wat")).toBe(en["err.internal"]);
  });
});

describe("throughput", () => {
  it("computes bytes per second", () => {
    const a = { up: 0, down: 0, at: 0 };
    expect(rate(null, a)).toBeNull();
    expect(rate(a, { up: 1000, down: 4000, at: 2000 })).toEqual({ up: 500, down: 2000 });
  });

  it("drops a reading when counters reset or time stalls", () => {
    expect(rate({ up: 10, down: 10, at: 0 }, { up: 1, down: 20, at: 1000 })).toBeNull();
    expect(rate({ up: 1, down: 1, at: 5 }, { up: 2, down: 2, at: 5 })).toBeNull();
  });

  it("keeps a fixed window", () => {
    let s: { up: number; down: number }[] = [];
    for (let i = 0; i < MAX_SAMPLES + 10; i++) s = push(s, { up: i, down: i });
    expect(s).toHaveLength(MAX_SAMPLES);
    expect(s[0]?.up).toBe(10);
  });
});

describe("validate", () => {
  it("accepts domains", () => {
    for (const d of ["example.com", "*.example.com", "a-b.co.uk", "xn--p1ai.xn--p1ai"]) expect(isDomain(d), d).toBe(true);
    for (const d of ["", "localhost", "-a.com", "a..com", "example.", "1.2.3.4", "a b.com", "*."]) expect(isDomain(d), d).toBe(false);
  });

  it("accepts CIDRs and addresses", () => {
    for (const c of ["10.0.0.0/8", "192.168.1.1", "::1", "2001:db8::/32", "fe80::1/64"]) expect(isCIDR(c), c).toBe(true);
    for (const c of ["10.0.0.0/33", "256.1.1.1", "1.2.3", "2001:db8::/129", "1.2.3.4/8/9", "abc", "1::2::3"]) expect(isCIDR(c), c).toBe(false);
  });

  it("classifies input", () => {
    expect(parseEntry(" Example.COM ")).toEqual({ kind: "domain", value: "example.com" });
    expect(parseEntry("10.0.0.0/8")).toEqual({ kind: "cidr", value: "10.0.0.0/8" });
    expect(parseEntry("https://Bank.IR/path")).toEqual({ kind: "domain", value: "bank.ir" });
    expect(parseEntry("nope")).toBeNull();
  });

  it("parses multiple sites and IPs", () => {
    const { ok, bad } = parseEntries("example.com, 10.0.0.0/8\nbank.ir https://news.example/x nope");
    expect(ok).toEqual([
      { kind: "domain", value: "example.com" },
      { kind: "cidr", value: "10.0.0.0/8" },
      { kind: "domain", value: "bank.ir" },
      { kind: "domain", value: "news.example" },
    ]);
    expect(bad).toEqual(["nope"]);
  });
});

describe("nodes and flags", () => {
  it("groups carriers under one node name", () => {
    const g = groupNodes([
      { id: "a", name: "Frankfurt", country: "DE", carrier: "h3", active: true, latencyMs: 42 },
      { id: "b", name: "Frankfurt", country: "DE", carrier: "cdnws", active: false, latencyMs: 118 },
      { id: "c", name: "Istanbul", country: "TR", carrier: "h3", active: false, latencyMs: 91 },
    ]);
    expect(g).toHaveLength(2);
    expect(g[0]?.name).toBe("Frankfurt");
    expect(g[0]?.ids).toEqual(["a", "b"]);
    expect(g[0]?.latencyMs).toBe(42);
  });

  it("builds flag emoji from ISO code", () => {
    expect(flagEmoji("DE")).toBe("🇩🇪");
    expect(flagEmoji("")).toBe("");
    expect(flagEmoji("x")).toBe("");
  });

  it("normalises ISO codes for the flag chip", () => {
    expect(flagCode(" de ")).toBe("DE");
    expect(flagCode("DEU")).toBe("");
    expect(flagCode(undefined)).toBe("");
  });

  it("labels account replacement distinctly from invite import", () => {
    expect(en["account.replace"]).toBe("Replace account");
    expect(fa["account.replace"]).not.toBe(fa["action.import"]);
  });
});
