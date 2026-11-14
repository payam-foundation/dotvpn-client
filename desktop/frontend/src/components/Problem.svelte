<script lang="ts">
  import { app } from "../lib/app.svelte";
  import { t } from "../lib/i18n";
</script>

{#if app.error}
  <div class="problem" role="alert">
    <p class="small danger">{app.errorMessage}</p>
    {#if app.error.message && app.error.message !== app.errorMessage && !app.errorMessage.includes(app.error.message)}
      <p class="mono muted"><span>{t(app.lang, "err.details")}</span> · {app.error.message}</p>
    {/if}
    {#if app.status.state === "failed"}
      <button class="link small" onclick={() => app.openReport()}>{t(app.lang, "report.link")}</button>
    {/if}
  </div>
{/if}

<style>
  .problem { display: flex; flex-direction: column; gap: var(--dot-space-1); }
  .link { align-self: flex-start; background: none; border: 0; padding: 0; color: var(--dot-fg); font: inherit; text-decoration: underline; }
  .mono { overflow-wrap: anywhere; font-size: 12px; line-height: 16px; }
</style>
