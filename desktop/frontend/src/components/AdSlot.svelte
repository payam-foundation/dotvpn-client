<script lang="ts">
  import { Browser } from "@wailsio/runtime";
  import type { Ad } from "../lib/types";

  let { ad }: { ad: Ad | null } = $props();

  function isSafeAdLink(link: string): boolean {
    const u = link.trim().toLowerCase();
    return u.startsWith("https://") || u.startsWith("http://");
  }

  async function openAd(ev: MouseEvent) {
    ev.preventDefault();
    const link = ad?.link?.trim() ?? "";
    if (!isSafeAdLink(link)) return;
    try {
      await Browser.OpenURL(link);
    } catch {
      // Mock / non-Wails preview: fall back to a normal tab.
      window.open(link, "_blank", "noopener,noreferrer");
    }
  }
</script>

{#if ad}
  <a
    class="ad"
    class:banner={ad.format === "banner"}
    class:card={ad.format === "card"}
    href={isSafeAdLink(ad.link || "") ? ad.link : undefined}
    role={isSafeAdLink(ad.link || "") ? undefined : "presentation"}
    onclick={openAd}
  >
    {#if ad.format === "banner" && ad.imageUrl}
      <img src={ad.imageUrl} alt={ad.title || ""} />
    {:else}
      {#if ad.iconUrl}<img class="icon" src={ad.iconUrl} alt={ad.title || ""} />{/if}
      <span class="copy">
        {#if ad.title}<span class="small">{ad.title}</span>{/if}
        {#if ad.desc}<span class="caption muted">{ad.desc}</span>{/if}
      </span>
    {/if}
  </a>
{/if}

<style>
  .ad {
    display: flex; align-items: center; gap: var(--dot-space-3);
    width: 100%; text-decoration: none; color: inherit;
    border: 1px solid var(--dot-border); border-radius: var(--dot-radius-md);
    background: var(--dot-surface); overflow: hidden;
    min-height: 56px; cursor: pointer;
  }
  .ad.banner { padding: 0; }
  .ad.banner img { width: 100%; height: 56px; object-fit: cover; display: block; }
  .ad.card { padding: var(--dot-space-3); }
  .icon { width: 36px; height: 36px; border-radius: 8px; object-fit: cover; }
  .copy { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
</style>
