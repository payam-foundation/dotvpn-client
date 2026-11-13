<script lang="ts">
  import { api } from "../lib/api";
  import { app } from "../lib/app.svelte";
  import { digits } from "../lib/format";
  import { t } from "../lib/i18n";
  import AdSlot from "../components/AdSlot.svelte";
  import Flag from "../components/Flag.svelte";
  import Icon from "../components/Icon.svelte";
  import Problem from "../components/Problem.svelte";
  import type { Ad, Relay } from "../lib/types";

  const lang = $derived(app.lang);
  let draft = $state("");
  let relayAd = $state<Ad | null>(null);
  let homeHadRelayAd = $state(false);
  let catalogOpen = $state(false);
  let privateOpen = $state(false);
  let copied = $state("");

  const catalog = $derived(
    app.relays.filter((r) => r.tier === "system" || (r.public && !isPrivate(r))),
  );
  const privateRelays = $derived(app.relays.filter((r) => isPrivate(r)));

  function isPrivate(r: Relay): boolean {
    return r.optedIn && !r.public && r.tier !== "system";
  }

  function healthLabel(h: string): string {
    return h === "ok" ? t(lang, "relay.label.good") : t(lang, "relay.label.failed");
  }

  function badge(r: Relay): string {
    if (r.tier === "system") return "";
    if (r.public) return t(lang, "relay.label.donated");
    return t(lang, "relay.label.private");
  }

  async function add() {
    const v = draft.trim();
    if (!v) return;
    if (await app.addRelay(v)) draft = "";
  }

  async function copyInvite(r: Relay) {
    if (!r.invite) return;
    try {
      await navigator.clipboard.writeText(r.invite);
      copied = r.id;
      setTimeout(() => {
        if (copied === r.id) copied = "";
      }, 1500);
    } catch {
      // clipboard optional
    }
  }

  const activeRelayId = $derived(
    app.status.viaRelay
      ? app.status.viaRelayId || app.relays.find((r) => r.name === app.status.viaRelay)?.id || ""
      : "",
  );
  $effect(() => {
    const rid = activeRelayId;
    homeHadRelayAd = false;
    relayAd = null;
    if (!rid) return;
    void (async () => {
      try {
        const home = await api.ads("home", rid);
        homeHadRelayAd = home.some((a) => a.issuer === "relay" && a.relayId === rid);
        if (homeHadRelayAd) return;
        const ads = await api.ads("relay", rid);
        relayAd = ads.find((a) => a.issuer === "relay") ?? null;
      } catch {
        relayAd = null;
      }
    })();
  });
</script>

<section class="page">
  <header class="head">
    <Icon name="servers" size={22} />
    <h1 class="h3">{t(lang, "relay.title")}</h1>
  </header>
  <Problem />

  <button class="server card" type="button" onclick={() => (catalogOpen = true)} disabled={catalog.length === 0}>
    <span class="count mono">{digits(catalog.length, lang)}</span>
    <span class="name small">{t(lang, "relay.ours")}</span>
    <span class="chev muted" aria-hidden="true">›</span>
  </button>

  <button class="server card" type="button" onclick={() => (privateOpen = true)}>
    <span class="count mono">{digits(privateRelays.length, lang)}</span>
    <span class="name small">{t(lang, "relay.privateTitle")}</span>
    <span class="chev muted" aria-hidden="true">›</span>
  </button>

  <label class="add">
    <span class="caption muted">{t(lang, "relay.paste")}</span>
    <div class="row-input">
      <input class="field mono" dir="ltr" spellcheck="false" autocomplete="off" placeholder="dvr://" bind:value={draft} />
      <button class="btn" onclick={add}>{t(lang, "action.add")}</button>
    </div>
  </label>

  {#if relayAd && !homeHadRelayAd}
    <div class="ad-dock">
      <AdSlot ad={relayAd} />
    </div>
  {/if}
</section>

{#if catalogOpen}
  <div class="sheet" role="dialog" aria-label={t(lang, "relay.ours")}>
    <button class="backdrop" aria-label={t(lang, "action.back")} onclick={() => (catalogOpen = false)}></button>
    <div class="panel card">
      <header class="sheet-head">
        <h2 class="title">{t(lang, "relay.ours")}</h2>
        <button class="btn quiet compact" onclick={() => (catalogOpen = false)}>{t(lang, "action.back")}</button>
      </header>
      <ul class="list">
        {#each catalog as r (r.id)}
          <li class="row">
            <Flag cc={r.country} />
            <span class="name">
              <span class="small">{r.name}</span>
              <span class="caption muted">
                {r.country || "—"}
                {#if badge(r)} · {badge(r)}{/if}
              </span>
            </span>
            <span class="pill caption" class:ok={r.health === "ok"} class:bad={r.health !== "ok"}>
              {healthLabel(r.health)}
            </span>
          </li>
        {/each}
      </ul>
    </div>
  </div>
{/if}

{#if privateOpen}
  <div class="sheet" role="dialog" aria-label={t(lang, "relay.privateTitle")}>
    <button class="backdrop" aria-label={t(lang, "action.back")} onclick={() => (privateOpen = false)}></button>
    <div class="panel card">
      <header class="sheet-head">
        <h2 class="title">{t(lang, "relay.privateTitle")}</h2>
        <button class="btn quiet compact" onclick={() => (privateOpen = false)}>{t(lang, "action.back")}</button>
      </header>
      {#if privateRelays.length === 0}
        <p class="caption muted empty-pad">{t(lang, "relay.privateEmpty")}</p>
      {:else}
        <ul class="list">
          {#each privateRelays as r (r.id)}
            <li class="row">
              <Flag cc={r.country} />
              <span class="name">
                <span class="small">{r.name}</span>
                <span class="caption muted">{badge(r)} · {healthLabel(r.health)}</span>
              </span>
              {#if r.invite}
                <button
                  class="btn quiet icon"
                  aria-label={t(lang, "action.copy")}
                  onclick={() => void copyInvite(r)}
                >
                  <Icon name="copy" size={18} />
                </button>
              {/if}
              <button class="btn quiet" onclick={() => app.removeRelay(r.id)}>{t(lang, "action.remove")}</button>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </div>
{/if}

<style>
  .page {
    display: flex; flex-direction: column; gap: var(--dot-space-4);
    min-height: 100%;
    padding: var(--dot-space-5) var(--dot-space-4) var(--dot-space-4);
    background:
      radial-gradient(70% 40% at 100% 0%, color-mix(in srgb, var(--dot-fg) 4%, transparent), transparent 65%);
  }
  .ad-dock { margin-top: auto; padding-top: var(--dot-space-3); }
  .head { display: flex; align-items: center; gap: var(--dot-space-2); }
  .server {
    display: flex; align-items: center; gap: var(--dot-space-3); width: 100%;
    min-height: 60px; padding: var(--dot-space-3) var(--dot-space-4);
    background: var(--dot-surface); border: 1px solid var(--dot-border); text-align: start; cursor: pointer;
  }
  .server:hover:not(:disabled) { background: var(--dot-surface-2); }
  .server:disabled { opacity: 0.5; cursor: default; }
  .count {
    flex: none; display: inline-flex; align-items: center; justify-content: center;
    min-width: 36px; height: 28px; padding: 0 var(--dot-space-2); box-sizing: border-box;
    border: 1px solid var(--dot-border); border-radius: var(--dot-radius-pill);
    font-size: 13px; font-variant-numeric: tabular-nums;
  }
  .name { flex: 1; display: flex; flex-direction: column; min-width: 0; gap: 1px; }
  .chev { flex: none; font-size: 18px; line-height: 1; }
  :global([dir="rtl"]) .chev { transform: scaleX(-1); }
  .add { display: flex; flex-direction: column; gap: var(--dot-space-2); }
  .row-input { display: flex; gap: var(--dot-space-2); }
  .row-input .field { flex: 1; }
  .sheet { position: fixed; inset: 0; z-index: 20; display: flex; align-items: flex-end; justify-content: center; }
  .backdrop { position: absolute; inset: 0; background: rgba(0,0,0,0.4); border: 0; backdrop-filter: blur(2px); }
  .panel {
    position: relative; width: min(680px, 100%); max-height: 72vh; overflow: auto;
    border-radius: var(--dot-radius-lg) var(--dot-radius-lg) 0 0; padding: 0;
    box-shadow: var(--dot-shadow-menu);
  }
  .sheet-head {
    display: flex; align-items: center; justify-content: space-between; gap: var(--dot-space-3);
    padding: var(--dot-space-3) var(--dot-space-4);
    border-bottom: 1px solid var(--dot-border);
    position: sticky; top: 0; background: var(--dot-surface); z-index: 1;
  }
  .title { margin: 0; font: 600 15px / 20px var(--dot-font-sans); letter-spacing: -0.01em; }
  .compact { min-height: 36px; padding: 0 var(--dot-space-3); font-size: 13px; }
  .list { list-style: none; margin: 0; padding: 0; }
  .row {
    display: flex; align-items: center; gap: var(--dot-space-3); width: 100%; min-height: 56px;
    padding: var(--dot-space-2) var(--dot-space-4);
  }
  .row + .row { border-top: 1px solid var(--dot-border); }
  .pill {
    flex: none; padding: 2px 8px; border-radius: var(--dot-radius-pill);
    border: 1px solid var(--dot-border); color: var(--dot-fg-muted);
  }
  .pill.ok { color: var(--dot-signal-fg); border-color: color-mix(in srgb, var(--dot-signal) 45%, var(--dot-border)); }
  .pill.bad { color: var(--dot-danger); border-color: color-mix(in srgb, var(--dot-danger) 35%, var(--dot-border)); }
  .icon { padding: 0 var(--dot-space-2); min-height: 36px; }
  .empty-pad { padding: var(--dot-space-4); margin: 0; }
</style>
