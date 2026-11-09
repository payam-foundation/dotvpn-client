<script lang="ts">
  import { api } from "../lib/api";
  import { app } from "../lib/app.svelte";
  import { t } from "../lib/i18n";
  import Hop from "../components/Hop.svelte";
  import InviteForm from "../components/InviteForm.svelte";
  import Problem from "../components/Problem.svelte";

  const lang = $derived(app.lang);
  let working = $state(false);

  async function install() {
    working = true;
    const ok = await app.run(() => api.installHelper());
    working = false;
    if (ok) {
      app.error = null;
      await app.refresh();
    }
  }
</script>

<!-- First run: one line, one action. Helper first, then an invite. -->
<section class="empty">
  <Hop state="off" size={64} dark={app.dark} mono label={t(lang, "app.name")} />
  {#if app.helper === false}
    <div class="text">
      <h1 class="h2">{t(lang, app.error?.code === "version" ? "first.updateTitle" : "first.title")}</h1>
      <p class="small muted">{t(lang, app.error?.code === "version" ? "first.updateBody" : "first.body")}</p>
    </div>
    {#if app.error?.code !== "version"}
      <Problem />
    {/if}
    <button class="btn primary block tall" onclick={install} disabled={working}>
      {working ? t(lang, "first.working") : t(lang, app.error?.code === "version" ? "first.update" : "first.setup")}
    </button>
  {:else}
    <div class="text">
      <h1 class="h2">{t(lang, "first.inviteTitle")}</h1>
      <p class="small muted">{t(lang, "first.inviteBody")}</p>
    </div>
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
  .empty > :global(.btn) { max-width: 320px; }
</style>
