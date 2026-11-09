<script lang="ts">
  import type { Sample } from "../lib/throughput";
  import { MAX_SAMPLES } from "../lib/throughput";

  let { samples, label }: { samples: Sample[]; label: string } = $props();

  const W = 300;
  const H = 64;
  const FLOOR = 4 * 1024; // a flat idle line stays flat instead of scaling noise up

  const max = $derived(Math.max(FLOOR, ...samples.flatMap((s) => [s.down, s.up])));

  function line(pick: (s: Sample) => number): string {
    const n = samples.length;
    const offset = MAX_SAMPLES - n;
    return samples
      .map((s, i) => {
        const x = ((offset + i) / (MAX_SAMPLES - 1)) * W;
        const y = H - 2 - (pick(s) / max) * (H - 4);
        return `${i ? "L" : "M"}${x.toFixed(1)} ${y.toFixed(1)}`;
      })
      .join(" ");
  }
</script>

<svg viewBox="0 0 {W} {H}" preserveAspectRatio="none" role="img" aria-label={label}>
  <line x1="0" y1={H - 0.5} x2={W} y2={H - 0.5} class="base" />
  {#if samples.length > 1}
    <path d={line((s) => s.up)} class="up" />
    <path d={line((s) => s.down)} class="down" />
  {/if}
</svg>

<style>
  svg { display: block; width: 100%; height: 64px; direction: ltr; }
  path, line { fill: none; vector-effect: non-scaling-stroke; stroke-linecap: round; stroke-linejoin: round; }
  .base { stroke: var(--dot-border); stroke-width: 1; }
  .down { stroke: var(--dot-fg); stroke-width: 1.75; }
  .up { stroke: var(--dot-fg-muted); stroke-width: 1.25; stroke-dasharray: 3 3; }
</style>
