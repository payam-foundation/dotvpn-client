<script lang="ts">
  import { app } from "../lib/app.svelte";
  import { flagEmoji } from "../lib/flag";
  import { t } from "../lib/i18n";
  import Problem from "../components/Problem.svelte";

  const lang = $derived(app.lang);
  let draft = $state("");

  const curated = $derived(app.relays.filter((r) => r.tier === "curated"));
  const community = $derived(app.relays.filter((r) => r.tier !== "curated"));

  async function add() {
    const v = draft.trim();
    if (!v) return;
    if (await app.addRelay(v)) draft = "";
  }
</script>

<section class="page">
  <header class="head">
    <h1 class="h3">{t(lang, "relay.title")}</h1>
    <p class="small muted">{t(lang, "relay.body")}</p>
  </header>
  <Problem />

  <div class="group">
    <h2 class="caption muted">{t(lang, "relay.curated")}</h2>
    {#if curated.length === 0}
      <p class="small muted">{t(lang, "relay.empty")}</p>
    {:else}
      <ul class="card list">
        {#each curated as r (r.id)}
          <li class="row">
            <span class="flag" aria-hidden="true">{flagEmoji(r.country) || "·"}</span>
            <span class="name">
              <span class="small">{r.name}</span>
              <span class="caption muted">{r.country || "—"}</span>
            </span>
            <span class="pill caption" class:ok={r.health === "ok"} class:warn={r.health === "degraded"} class:bad={r.health === "offline"}>
              {t(lang, `relay.health.${r.health}`)}
            </span>
          </li>
        {/each}
      </ul>
    {/if}
  </div>

  <div class="group">
    <h2 class="caption muted">{t(lang, "relay.community")}</h2>
    <label class="add">
      <span class="caption muted">{t(lang, "relay.paste")}</span>
      <div class="row-input">
        <input class="field mono" dir="ltr" spellcheck="false" autocomplete="off" placeholder="dotvpn-relay://" bind:value={draft} />
        <button class="btn" onclick={add}>{t(lang, "action.add")}</button>
      </div>
    </label>
    {#if community.length}
      <ul class="card list">
        {#each community as r (r.id)}
          <li class="row">
            <span class="flag" aria-hidden="true">{flagEmoji(r.country) || "·"}</span>
            <span class="name">
              <span class="small">{r.name}</span>
              <span class="caption muted">{r.country || "—"} · {t(lang, `relay.health.${r.health}`)}</span>
            </span>
            <button class="btn quiet" onclick={() => app.removeRelay(r.id)}>{t(lang, "action.remove")}</button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</section>

<style>
  .page {
    display: flex; flex-direction: column; gap: var(--dot-space-4);
    padding: var(--dot-space-5) var(--dot-space-4) var(--dot-space-4);
    background:
      radial-gradient(70% 40% at 100% 0%, color-mix(in srgb, var(--dot-fg) 4%, transparent), transparent 65%);
  }
  .head { display: flex; flex-direction: column; gap: var(--dot-space-2); }
  .group { display: flex; flex-direction: column; gap: var(--dot-space-2); }
  .list { list-style: none; margin: 0; padding: 0; overflow: hidden; }
  .row {
    display: flex; align-items: center; gap: var(--dot-space-3);
    min-height: 60px; padding: var(--dot-space-2) var(--dot-space-4);
  }
  .row + .row { border-top: 1px solid var(--dot-border); }
  .flag { flex: none; width: 1.75em; text-align: center; font-size: 20px; }
  .name { flex: 1; display: flex; flex-direction: column; min-width: 0; gap: 1px; }
  .pill {
    flex: none; padding: 2px 8px; border-radius: var(--dot-radius-pill);
    border: 1px solid var(--dot-border); color: var(--dot-fg-muted);
  }
  .pill.ok { color: var(--dot-signal-fg); border-color: color-mix(in srgb, var(--dot-signal) 45%, var(--dot-border)); }
  .pill.warn { color: var(--dot-warning); border-color: color-mix(in srgb, var(--dot-warning) 35%, var(--dot-border)); }
  .pill.bad { color: var(--dot-danger); border-color: color-mix(in srgb, var(--dot-danger) 35%, var(--dot-border)); }
  .add { display: flex; flex-direction: column; gap: var(--dot-space-2); }
  .row-input { display: flex; gap: var(--dot-space-2); }
  .row-input .field { flex: 1; }
</style>
