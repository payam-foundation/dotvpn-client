const label = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/i;

/** A hostname or domain as typed by a person: example.com, *.example.com. */
export function isDomain(s: string): boolean {
  const host = s.startsWith("*.") ? s.slice(2) : s;
  if (host.length === 0 || host.length > 253) return false;
  const parts = host.split(".");
  return parts.length >= 2 && parts.every((p) => label.test(p)) && !/^\d+$/.test(parts[parts.length - 1] ?? "");
}

function isV4(s: string): boolean {
  const p = s.split(".");
  return p.length === 4 && p.every((x) => /^\d{1,3}$/.test(x) && Number(x) <= 255);
}

function isV6(s: string): boolean {
  if (!/^[0-9a-f:]+$/i.test(s) || !s.includes(":") || s.split("::").length > 2) return false;
  const groups = s.split(":");
  const compressed = s.includes("::");
  if (groups.length > 8 || (!compressed && groups.length !== 8)) return false;
  return groups.every((g) => g.length <= 4);
}

/** A CIDR or a bare address (treated as a single host by the helper). */
export function isCIDR(s: string): boolean {
  const [addr = "", bits, extra] = s.split("/");
  if (extra !== undefined) return false;
  const v4 = isV4(addr);
  if (!v4 && !isV6(addr)) return false;
  if (bits === undefined) return true;
  return /^\d{1,3}$/.test(bits) && Number(bits) <= (v4 ? 32 : 128);
}

export type Entry = { kind: "domain" | "cidr"; value: string };

/** Strip scheme/path/port so pasted URLs become hostnames. Keeps CIDR suffixes. */
function hostFromURL(raw: string): string {
  let v = raw.trim().toLowerCase();
  const hadScheme = /^[a-z][a-z0-9+.-]*:\/\//.test(v);
  v = v.replace(/^[a-z][a-z0-9+.-]*:\/\//, "");
  if (hadScheme) {
    v = (v.split("/")[0] ?? v).split("?")[0] ?? v;
  }
  if (v.startsWith("[") && v.includes("]")) {
    return v.slice(1, v.indexOf("]"));
  }
  // host:port — only peel a trailing decimal port (not IPv6)
  if (/^\d{1,3}(\.\d{1,3}){3}:\d+$/.test(v) || (/^[a-z0-9.-]+:\d+$/i.test(v) && !v.includes("::"))) {
    v = v.replace(/:\d+$/, "");
  }
  return v;
}

export function parseEntry(raw: string): Entry | null {
  const v = hostFromURL(raw);
  if (!v) return null;
  if (isCIDR(v)) return { kind: "cidr", value: v };
  if (isDomain(v)) return { kind: "domain", value: v };
  return null;
}

/** Split a paste of domains/IPs on commas, whitespace, or newlines. */
export function parseEntries(raw: string): { ok: Entry[]; bad: string[] } {
  const parts = raw.split(/[\s,;]+/).map((p) => p.trim()).filter(Boolean);
  const ok: Entry[] = [];
  const bad: string[] = [];
  const seen = new Set<string>();
  for (const p of parts) {
    const e = parseEntry(p);
    if (!e) {
      bad.push(p);
      continue;
    }
    const k = `${e.kind}:${e.value}`;
    if (seen.has(k)) continue;
    seen.add(k);
    ok.push(e);
  }
  return { ok, bad };
}
