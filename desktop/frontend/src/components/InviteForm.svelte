<script lang="ts">
  import { api } from "../lib/api";
  import { app } from "../lib/app.svelte";
  import { middleTruncate } from "../lib/format";
  import { t } from "../lib/i18n";
  import Icon from "./Icon.svelte";
  import Problem from "./Problem.svelte";

  let { ondone }: { ondone: () => void } = $props();

  let value = $state("");
  let relayURI = $state("");
  let showRelay = $state(false);
  let relayOpen = $state(false);
  let focused = $state(false);
  let busy = $state(false);

  const lang = $derived(app.lang);
  const shown = $derived(focused ? value : middleTruncate(value, 38));

  function extractInvite(raw: string): string {
    const m = /dv:\/\/[A-Za-z0-9_-]+/.exec(raw.replace(/[ \t\n\r]/g, ""));
    return m?.[0] ?? raw.trim();
  }

  function extractRelay(raw: string): string {
    const m = /dvr:\/\/[A-Za-z0-9_-]+/.exec(raw.replace(/[ \t\n\r]/g, ""));
    return m?.[0] ?? raw.trim();
  }

  async function paste() {
    try {
      value = extractInvite(await api.paste());
    } catch {
      // clipboard optional
    }
  }

  async function pasteRelay() {
    try {
      relayURI = extractRelay(await api.paste());
      relayOpen = true;
    } catch {
      // clipboard optional
    }
  }

  async function submit(e: Event) {
    e.preventDefault();
    const uri = extractInvite(value);
    if (!uri || busy) return;
    value = uri;
    busy = true;
    let needsRelay = false;
    const ok = await app.run(async () => {
      const r = await api.importInvite(uri);
      needsRelay = !!r?.needsRelay;
      const relay = extractRelay(relayURI);
      if (relay) {
        await api.addRelay(relay);
        needsRelay = false;
      }
    });
    busy = false;
    if (!ok) return;
    if (needsRelay && !extractRelay(relayURI)) {
      showRelay = true;
      relayOpen = true;
      app.notice = "";
      return;
    }
    value = "";
    relayURI = "";
    showRelay = false;
    relayOpen = false;
    app.notice = "invite.done";
    await app.refresh();
    ondone();
  }
</script>

<form onsubmit={submit}>
  <label class="small muted" for="invite">{t(lang, "invite.label")}</label>
  <div class="row">
    <input
      id="invite"
      class="field mono"
      dir="ltr"
      spellcheck="false"
      autocomplete="off"
      autocapitalize="off"
      placeholder={t(lang, "invite.placeholder")}
      value={shown}
      oninput={(e) => (value = e.currentTarget.value)}
      onfocus={() => (focused = true)}
      onblur={() => (focused = false)}
    />
    <button type="button" class="btn quiet" onclick={paste} aria-label={t(lang, "action.paste")}>
      <Icon name="paste" /><span class="small">{t(lang, "action.paste")}</span>
    </button>
  </div>
  <p class="caption muted">{t(lang, "invite.hint")}</p>

  {#if showRelay}
    <p class="caption muted reach">{t(lang, "invite.needsRelay")}</p>
    {#if !relayOpen}
      <button type="button" class="linkish" onclick={() => (relayOpen = true)}>
        {t(lang, "invite.bypassLink")}
      </button>
    {:else}
      <label class="small muted" for="relay-adv">{t(lang, "invite.relayLabel")}</label>
      <div class="row">
        <input
          id="relay-adv"
          class="field mono"
          dir="ltr"
          spellcheck="false"
          autocomplete="off"
          autocapitalize="off"
          placeholder={t(lang, "invite.relayPlaceholder")}
          bind:value={relayURI}
        />
        <button type="button" class="btn quiet" onclick={pasteRelay} aria-label={t(lang, "action.paste")}>
          <Icon name="paste" /><span class="small">{t(lang, "action.paste")}</span>
        </button>
      </div>
      <p class="caption muted">{t(lang, "invite.relayHint")}</p>
    {/if}
  {/if}

  <Problem />
  <button class="btn primary block" type="submit" disabled={!value.trim() || busy}>{t(lang, "action.import")}</button>
</form>

<style>
  form { display: flex; flex-direction: column; gap: var(--dot-space-3); }
  .row { display: flex; gap: var(--dot-space-2); }
  .row .field { flex: 1; min-width: 0; text-overflow: ellipsis; }
  .row .btn { padding: 0 var(--dot-space-3); }
  .reach { text-align: start; }
  .linkish {
    align-self: flex-start; padding: 0; border: 0; background: none;
    color: var(--dot-fg); font: 500 13px / 18px var(--dot-font-sans);
    text-decoration: underline; text-underline-offset: 3px; cursor: pointer;
  }
  .linkish:hover { color: var(--dot-fg-muted); }
</style>
