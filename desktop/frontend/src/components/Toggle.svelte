<script lang="ts">
  let { checked, label, hint = "", disabled = false, onchange }: {
    checked: boolean;
    label: string;
    hint?: string;
    disabled?: boolean;
    onchange: (v: boolean) => void;
  } = $props();
</script>

<button class="row" role="switch" aria-checked={checked} {disabled} onclick={() => onchange(!checked)}>
  <span class="text">
    <span class="small">{label}</span>
    {#if hint}<span class="caption muted">{hint}</span>{/if}
  </span>
  <span class="track" class:on={checked} aria-hidden="true"><span class="knob"></span></span>
</button>

<style>
  .row {
    display: flex; align-items: center; justify-content: space-between; gap: var(--dot-space-4);
    width: 100%; min-height: 56px; padding: var(--dot-space-2) var(--dot-space-4);
    background: none; border: 0; text-align: start;
  }
  .row:disabled { opacity: 0.5; cursor: default; }
  .text { display: flex; flex-direction: column; }
  /* "On" is fg, not lime: these are settings, not connection state. */
  .track {
    flex: none; width: 40px; height: 24px; padding: 2px; border-radius: 999px;
    background: var(--dot-off); border: 1px solid var(--dot-border);
    transition: background var(--dot-dur-fast) var(--dot-ease);
  }
  .track.on { background: var(--dot-fg); border-color: var(--dot-fg); }
  .knob {
    display: block; width: 18px; height: 18px; border-radius: 50%; background: var(--dot-bg);
    transition: transform var(--dot-dur-fast) var(--dot-ease);
  }
  .track.on .knob { transform: translateX(16px); }
  :global([dir="rtl"]) .track.on .knob { transform: translateX(-16px); }
</style>
