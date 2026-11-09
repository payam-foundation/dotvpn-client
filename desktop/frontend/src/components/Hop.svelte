<script lang="ts">
  import type { State } from "../lib/types";

  /**
   * The Hop status mark, drawn from the geometry in design/GUIDELINES.md.
   * The path always rises left to right; it does not mirror in RTL.
   */
  let {
    state = "off",
    size = 40,
    dark = false,
    mono = false,
    label = "",
  }: { state?: State; size?: number; dark?: boolean; mono?: boolean; label?: string } = $props();

  const dots: [number, number][] = [
    [15.02, 45.51],
    [16.1, 36.59],
    [19.2, 28.17],
    [25.36, 21.74],
    [33.88, 19.08],
  ];
  const path = "M15 46 Q16 18 38 19";

  const moving = $derived(state === "connecting" || state === "switching");
  const on = $derived(state === "connected");
  const bigFill = $derived(mono ? "var(--dot-fg)" : on ? "var(--dot-signal)" : state === "failed" ? "var(--dot-danger)" : "var(--dot-off)");
  // Lime always sits on ink: on light, inside a rim.
  const rim = $derived(on && !mono && !dark);
  const trail = $derived(on || mono ? "var(--dot-fg)" : "var(--dot-off)");

  const reduced = typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
</script>

<svg viewBox="0 0 64 64" width={size} height={size} role="img" aria-label={label || state} class:on class:fail={state === "failed"}>
  {#each dots as [x, y]}
    <circle cx={x} cy={y} r="2.6" fill={trail} class="trail" />
  {/each}
  <circle cx="14" cy="48" r="6" fill="var(--dot-fg)" />
  {#if rim}
    <circle cx="46" cy="20" r="10.5" fill="#0A0A0A" class="big" />
    <circle cx="46" cy="20" r="6.5" fill="var(--dot-signal)" class="big" />
  {:else}
    <circle cx="46" cy="20" r="9.5" fill={bigFill} class="big" />
  {/if}
  {#if moving && !reduced}
    <circle r="3.5" fill="var(--dot-signal)" stroke={dark ? "none" : "#0A0A0A"} stroke-width="1.5" class="traveller">
      <animateMotion dur="700ms" repeatCount="indefinite" path={path} calcMode="spline" keyPoints="0;1" keyTimes="0;1" keySplines="0.4 0 0.2 1" />
    </circle>
  {/if}
</svg>

<style>
  svg { flex: none; overflow: visible; }
  .trail, .big { transition: fill var(--dot-dur-base) var(--dot-ease); }
  .on .big { animation: settle var(--dot-dur-base) var(--dot-ease); transform-origin: 46px 20px; }
  .fail .big { transition-duration: var(--dot-dur-fast); }
  @keyframes settle { from { transform: scale(0.9); } to { transform: scale(1); } }
</style>
