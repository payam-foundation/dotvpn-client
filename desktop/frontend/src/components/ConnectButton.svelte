<script lang="ts">
  import type { State } from "../lib/types";

  let {
    state,
    label,
    onToggle,
  }: {
    state: State;
    label: string;
    onToggle: () => void;
  } = $props();

  const busy = $derived(state === "connecting" || state === "switching");
  const on = $derived(state === "connected");
  const failed = $derived(state === "failed");
</script>

<button
  class="orb"
  class:on
  class:busy
  class:failed
  role="switch"
  aria-checked={on}
  aria-busy={busy}
  aria-label={label}
  onclick={onToggle}
>
  {#if busy}
    <span class="ring" aria-hidden="true"></span>
    <span class="hype a" aria-hidden="true"></span>
    <span class="hype b" aria-hidden="true"></span>
    <span class="hype c" aria-hidden="true"></span>
  {/if}
  <span class="label">{label}</span>
</button>

<style>
  .orb {
    position: relative;
    align-self: center;
    margin-inline: auto;
    display: grid;
    place-items: center;
    width: 148px; height: 148px; border-radius: 50%;
    flex: none;
    border: 1.5px solid var(--dot-border);
    background:
      radial-gradient(120% 80% at 30% 20%, color-mix(in srgb, var(--dot-fg) 6%, transparent), transparent 55%),
      var(--dot-surface);
    color: var(--dot-fg);
    font: var(--dot-text-h3);
    letter-spacing: var(--dot-tracking-h3);
    cursor: pointer;
    transition:
      background var(--dot-dur-base) var(--dot-ease),
      border-color var(--dot-dur-base) var(--dot-ease),
      color var(--dot-dur-base) var(--dot-ease),
      transform var(--dot-dur-fast) var(--dot-ease),
      box-shadow var(--dot-dur-base) var(--dot-ease);
  }
  .orb:hover { background: var(--dot-surface-2); }
  .orb:active { transform: scale(0.98); }
  .orb.on {
    background: var(--dot-fg);
    border-color: var(--dot-fg);
    color: var(--dot-bg);
    box-shadow: 0 12px 32px color-mix(in srgb, var(--dot-fg) 18%, transparent);
  }
  .orb.busy { border-color: color-mix(in srgb, var(--dot-fg) 35%, var(--dot-border)); }
  .orb.failed { border-color: var(--dot-danger); }
  .label { position: relative; z-index: 2; font-weight: 600; }
  .ring {
    position: absolute; inset: -12px; border-radius: 50%;
    border: 1.5px solid color-mix(in srgb, var(--dot-fg) 28%, transparent);
    animation: pulse 1.4s var(--dot-ease) infinite;
  }
  .hype {
    position: absolute; width: 8px; height: 8px; border-radius: 50%;
    background: var(--dot-fg); z-index: 1;
    top: 50%; left: 50%; margin: -4px;
    animation: orbit 1.4s linear infinite;
    box-shadow: 0 0 12px color-mix(in srgb, var(--dot-fg) 45%, transparent);
  }
  .hype.b { animation-delay: -0.46s; opacity: 0.72; width: 6px; height: 6px; margin: -3px; }
  .hype.c { animation-delay: -0.92s; opacity: 0.4; width: 5px; height: 5px; margin: -2.5px; }
  @keyframes orbit {
    from { transform: rotate(0deg) translateX(80px) rotate(0deg); }
    to { transform: rotate(360deg) translateX(80px) rotate(-360deg); }
  }
  @keyframes pulse {
    0%, 100% { opacity: 0.35; transform: scale(0.98); }
    50% { opacity: 0.9; transform: scale(1.02); }
  }
  @media (prefers-reduced-motion: reduce) {
    .hype, .ring { animation: none; }
    .hype { display: none; }
  }
</style>
