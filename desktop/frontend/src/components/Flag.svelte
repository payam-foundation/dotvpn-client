<script lang="ts">
  import { flagCode, flagEmoji, flagRenders } from "../lib/flag";

  let { cc }: { cc?: string } = $props();

  const code = $derived(flagCode(cc));
  const emoji = $derived(code && flagRenders() ? flagEmoji(code) : "");
</script>

<span class="flag" aria-hidden="true">
  {#if emoji}
    <span class="emoji">{emoji}</span>
  {:else if code}
    <span class="chip">{code}</span>
  {:else}
    <span class="none">·</span>
  {/if}
</span>

<style>
  .flag { flex: none; display: inline-flex; align-items: center; justify-content: center; width: 36px; line-height: 1; }
  .emoji { font-size: 20px; }
  .chip {
    padding: 2px 6px; border: 1px solid var(--dot-border); border-radius: var(--dot-radius-pill);
    color: var(--dot-fg-muted); font: 500 11px / 14px var(--dot-font-mono); letter-spacing: 0.04em; direction: ltr;
  }
  .none { color: var(--dot-fg-muted); font-size: 20px; }
</style>
