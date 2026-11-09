/** ISO-3166 alpha-2 → regional-indicator emoji. No network fetch. */
export function flagEmoji(cc?: string): string {
  if (!cc || cc.length !== 2) return "";
  const a = cc.toUpperCase();
  if (!/^[A-Z]{2}$/.test(a)) return "";
  return String.fromCodePoint(...[...a].map((c) => 0x1f1e6 - 65 + c.charCodeAt(0)));
}
