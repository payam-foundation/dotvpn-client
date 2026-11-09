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
  </div>
{/if}

<style>
  .problem { display: flex; flex-direction: column; gap: var(--dot-space-1); }
  .mono { overflow-wrap: anywhere; font-size: 12px; line-height: 16px; }
</style>
