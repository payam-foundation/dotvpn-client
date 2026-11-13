/** ISO-3166 alpha-2 → regional-indicator emoji. No network fetch. */
export function flagEmoji(cc?: string): string {
  if (!cc || cc.length !== 2) return "";
  const a = cc.toUpperCase();
  if (!/^[A-Z]{2}$/.test(a)) return "";
  return String.fromCodePoint(...[...a].map((c) => 0x1f1e6 - 65 + c.charCodeAt(0)));
}

/** Normalised ISO alpha-2, or "" when cc is not a valid code. */
export function flagCode(cc?: string): string {
  const a = (cc ?? "").trim().toUpperCase();
  return /^[A-Z]{2}$/.test(a) ? a : "";
}

let renders: boolean | undefined;

/** True when this webview paints flag emoji in colour (Windows paints plain letters). Cached. */
export function flagRenders(): boolean {
  if (renders === undefined) renders = probeFlagEmoji();
  return renders;
}

function probeFlagEmoji(): boolean {
  if (typeof document === "undefined") return false;
  const ctx = document.createElement("canvas").getContext("2d", { willReadFrequently: true });
  if (!ctx) return false;
  ctx.canvas.width = 40;
  ctx.canvas.height = 40;
  ctx.textBaseline = "top";
  ctx.font = "28px sans-serif";
  ctx.fillText(flagEmoji("DE"), 0, 0);
  const px = ctx.getImageData(0, 0, 40, 40).data;
  for (let i = 0; i + 3 < px.length; i += 4) {
    const [r, g, b, a] = [px[i] ?? 0, px[i + 1] ?? 0, px[i + 2] ?? 0, px[i + 3] ?? 0];
    if (a > 0 && (r !== g || g !== b)) return true;
  }
  return false;
}
