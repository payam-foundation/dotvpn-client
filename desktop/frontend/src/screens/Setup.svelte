<script lang="ts">
  import { api } from "../lib/api";
  import { app } from "../lib/app.svelte";
  import { t } from "../lib/i18n";
  import Hop from "../components/Hop.svelte";
  import InviteForm from "../components/InviteForm.svelte";
  import Problem from "../components/Problem.svelte";
  import Toggle from "../components/Toggle.svelte";

  const lang = $derived(app.lang);
  const updating = $derived(app.helperUpdate);
  let working = $state(false);
  // Opt-in: first Setup only (privacy default). Later changes live in Settings.
  let shareDiagnostics = $state(false);

  async function install() {
    working = true;
    const wasUpdate = updating;
    const share = shareDiagnostics;
    const ok = await app.run(() => api.installHelper());
    working = false;
    if (ok) {
      app.error = null;
      await app.refresh();
      if (!wasUpdate && app.helper) {
        await app.saveSettings({ crashReporting: share });
      }
    }
  }

  async function setShareDiagnostics(v: boolean) {
    shareDiagnostics = v;
  }
</script>

<!-- First run: helper first, then an invite. Diagnostics only on first install. -->
<section class="empty">
  <Hop state="off" size={64} dark={app.dark} mono label={t(lang, "app.name")} />
  {#if app.helper === false}
    <div class="text">
      <h1 class="h2">{t(lang, updating ? "first.updateTitle" : "first.title")}</h1>
      <p class="small muted">{t(lang, updating ? "first.updateBody" : "first.body")}</p>
    </div>
    {#if app.error && app.error.code !== "version"}
      <Problem />
    {/if}
    {#if !updating}
      <div class="opt-in card">
        <Toggle
          checked={shareDiagnostics}
          label={t(lang, "first.diagnostics")}
          hint={t(lang, "first.diagnosticsHint")}
          onchange={setShareDiagnostics}
        />
      </div>
    {/if}
    <button class="btn primary block tall" onclick={install} disabled={working}>
      {working ? t(lang, "first.working") : t(lang, updating ? "first.update" : "first.setup")}
    </button>
  {:else}
    <div class="text">
      <h1 class="h2">{t(lang, "first.inviteTitle")}</h1>
      <p class="small muted">{t(lang, "first.inviteBody")}</p>
    </div>
    <Problem />
    <div class="form"><InviteForm ondone={() => (app.view = "home")} /></div>
  {/if}
</section>

<style>
  .empty {
    flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center;
    gap: var(--dot-space-5); padding: var(--dot-space-5) var(--dot-space-4); text-align: center;
  }
  .text { display: flex; flex-direction: column; gap: var(--dot-space-2); max-width: 320px; }
  .form { width: 100%; text-align: start; }
  .opt-in { width: 100%; max-width: 320px; text-align: start; overflow: hidden; }
  .empty > :global(.btn) { max-width: 320px; }
</style>
