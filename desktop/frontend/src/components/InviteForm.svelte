<script lang="ts">
  import { api } from "../lib/api";
  import { app } from "../lib/app.svelte";
  import { middleTruncate } from "../lib/format";
  import { t } from "../lib/i18n";
  import Icon from "./Icon.svelte";
  import Problem from "./Problem.svelte";

  let { ondone }: { ondone: () => void } = $props();

  let value = $state("");
  let focused = $state(false);
  let busy = $state(false);

  const lang = $derived(app.lang);
  // Middle-truncated while idle so both ends stay visible; full text while editing.
  const shown = $derived(focused ? value : middleTruncate(value, 38));

  async function paste() {
    try {
      value = (await api.paste()).trim();
    } catch {
      // No clipboard access: the field still takes ⌘V.
    }
  }

  async function submit(e: Event) {
    e.preventDefault();
    if (!value.trim() || busy) return;
    busy = true;
    const ok = await app.run(() => api.importInvite(value));
    busy = false;
    if (ok) {
      value = "";
      app.notice = "invite.done";
      await app.refresh();
      ondone();
    }
  }
</script>

<form onsubmit={submit}>
  <label class="small muted" for="invite">{t(lang, "invite.label")}</label>
  <div class="row">
    <!-- Always LTR, even in Persian: it is a URL. -->
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
  <Problem />
  <button class="btn primary block" type="submit" disabled={!value.trim() || busy}>{t(lang, "action.import")}</button>
</form>

<style>
  form { display: flex; flex-direction: column; gap: var(--dot-space-3); }
  .row { display: flex; gap: var(--dot-space-2); }
  .row .field { flex: 1; min-width: 0; text-overflow: ellipsis; }
  .row .btn { padding: 0 var(--dot-space-3); }
</style>
