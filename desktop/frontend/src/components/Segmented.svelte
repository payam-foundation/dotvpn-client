<script lang="ts" generics="T extends string">
  let { value, options, label, onchange }: {
    value: T;
    options: { value: T; label: string }[];
    label: string;
    onchange: (v: T) => void;
  } = $props();
</script>

<div class="seg" role="radiogroup" aria-label={label}>
  {#each options as o}
    <button role="radio" aria-checked={value === o.value} class="small" onclick={() => onchange(o.value)}>{o.label}</button>
  {/each}
</div>

<style>
  .seg { display: flex; gap: 2px; padding: 2px; background: var(--dot-surface-2); border-radius: var(--dot-radius-md); }
  button {
    flex: 1; min-height: 36px; padding: 0 var(--dot-space-3); border: 0; border-radius: 8px;
    background: transparent; color: var(--dot-fg-muted);
    transition: background var(--dot-dur-fast) var(--dot-ease);
  }
  button[aria-checked="true"] { background: var(--dot-bg); color: var(--dot-fg); box-shadow: 0 0 0 1px var(--dot-border); }
</style>
