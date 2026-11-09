// Renders the tray icons from the Hop geometry in design/GUIDELINES.md.
// Run: node scripts/icons.mjs   (needs design/scripts/node_modules, see design/README.md)
// Output is committed; macOS gets monochrome template images, Windows and
// Linux get colour ones that read on light and dark panels.
import { createRequire } from "node:module";
import { writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const require = createRequire(join(here, "../../../design/scripts/"));
const { Resvg } = require("@resvg/resvg-js");

const INK = "#0A0A0A";
const LIME = "#C6F432";
const MUTED = "#8F8F8F";
const DANGER = "#FF6B5E";

// Quadratic M15 46 Q16 18 38 19, sampled by arc length: a dot every 9 units.
function pathDots() {
  const P = [[15, 46], [16, 18], [38, 19]];
  const at = (t) => [0, 1].map((i) => (1 - t) ** 2 * P[0][i] + 2 * (1 - t) * t * P[1][i] + t * t * P[2][i]);
  const pts = [];
  let len = 0;
  let prev = at(0);
  pts.push({ len: 0, p: prev });
  for (let i = 1; i <= 400; i++) {
    const p = at(i / 400);
    len += Math.hypot(p[0] - prev[0], p[1] - prev[1]);
    pts.push({ len, p });
    prev = p;
  }
  const dots = [];
  for (let d = 4.5; d < len - 4; d += 9) dots.push(pts.find((s) => s.len >= d).p);
  return { dots, mid: pts.find((s) => s.len >= len / 2).p };
}
const { dots, mid } = pathDots();

function svg({ color, state }) {
  const base = color ? MUTED : "#000";
  let s = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">`;
  s += `<circle cx="14" cy="48" r="6" fill="${base}"/>`;
  for (const [x, y] of dots) s += `<circle cx="${x.toFixed(2)}" cy="${y.toFixed(2)}" r="2.6" fill="${base}"/>`;
  const ring = `<circle cx="46" cy="20" r="8" fill="none" stroke="${base}" stroke-width="3"/>`;
  if (state === "on") {
    s += color
      ? `<circle cx="46" cy="20" r="10.5" fill="${INK}"/><circle cx="46" cy="20" r="6.5" fill="${LIME}"/>`
      : `<circle cx="46" cy="20" r="9.5" fill="#000"/>`;
  } else if (state === "fail") {
    s += color
      ? `<circle cx="46" cy="20" r="9.5" fill="${DANGER}"/>`
      : ring + `<path d="M46 14v6" stroke="#000" stroke-width="3" stroke-linecap="round"/><circle cx="46" cy="25" r="1.6" fill="#000"/>`;
  } else {
    s += ring;
    if (state === "busy") {
      s += color
        ? `<circle cx="${mid[0]}" cy="${mid[1]}" r="5" fill="${INK}"/><circle cx="${mid[0]}" cy="${mid[1]}" r="3.5" fill="${LIME}"/>`
        : `<circle cx="${mid[0]}" cy="${mid[1]}" r="3.5" fill="#000"/>`;
    }
  }
  return s + "</svg>";
}

for (const state of ["off", "busy", "on", "fail"]) {
  for (const color of [false, true]) {
    const size = color ? 64 : 44;
    const png = new Resvg(svg({ color, state }), { fitTo: { mode: "width", value: size } }).render().asPng();
    writeFileSync(join(here, "../icons", `tray-${color ? "color" : "mono"}-${state}.png`), png);
  }
}
