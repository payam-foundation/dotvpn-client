<script lang="ts">
  // The UI icons from design/icons, 24px grid, currentColor.
  const files = import.meta.glob("../design/icons/*.svg", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

  let { name, size = 20 }: { name: string; size?: number } = $props();

  const svg = $derived(
    (files[`../design/icons/${name}.svg`] ?? "").replace(/width="24" height="24"/, `width="${size}" height="${size}" aria-hidden="true"`),
  );
</script>

<span class="icon">{@html svg}</span>

<style>
  .icon { display: inline-flex; flex: none; }
</style>
