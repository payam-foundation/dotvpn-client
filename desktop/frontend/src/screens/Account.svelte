<script lang="ts">
  import { app } from "../lib/app.svelte";
  import { digits, formatBytes } from "../lib/format";
  import { t } from "../lib/i18n";
  import Problem from "../components/Problem.svelte";

  const lang = $derived(app.lang);
  const a = $derived(app.account);
  const hasAccount = $derived(!!(a.name && a.name.trim()));

  const quotaLine = $derived.by(() => {
    if (!hasAccount) return "—";
    if (!a.quotaLimit) return t(lang, "account.unlimited");
    return `${formatBytes(a.quotaRemaining || 0, lang)} / ${formatBytes(a.quotaLimit, lang)}`;
  });

  const resetDays = $derived.by(() => {
    if (!a.quotaReset) return "";
    const ms = Date.parse(a.quotaReset) - app.now;
    if (!Number.isFinite(ms)) return "";
    const days = Math.max(0, Math.ceil(ms / 86_400_000));
    return t(lang, "account.resetIn", { n: digits(days, lang) });
  });

  const planEnd = $derived.by(() => {
    if (!a.expires) return "";
    const left = a.expires * 1000 - app.now;
    if (left < 0) return t(lang, "account.ended");
    const days = Math.ceil(left / 86_400_000);
    return t(lang, "account.endsIn", { n: digits(days, lang) });
  });

  async function logout() {
    if (!(await app.logout())) return;
  }
</script>

<section class="page">
  <header class="head">
    <h1 class="h3">{t(lang, "account.title")}</h1>
  </header>
  <Problem />

  {#if a.disabled}
    <p class="small danger">{t(lang, "account.disabled")}</p>
  {/if}

  {#if !hasAccount && app.status.configured}
    <p class="small muted">{t(lang, "account.unavailable")}</p>
  {/if}

  <div class="card list">
    <div class="row">
      <span class="caption muted">{t(lang, "account.name")}</span>
      <span class="small">{a.name?.trim() || "—"}</span>
    </div>
    <div class="row">
      <span class="caption muted">{t(lang, "account.quota")}</span>
      <span class="small">{quotaLine}</span>
      {#if resetDays}<span class="caption muted">{resetDays}</span>{/if}
    </div>
    {#if planEnd}
      <div class="row">
        <span class="caption muted">{t(lang, "account.plan")}</span>
        <span class="small">{planEnd}</span>
      </div>
    {/if}
    <div class="row">
      <span class="caption muted">{t(lang, "account.devices")}</span>
      <span class="small">
        {#if (a.maxDevices ?? 0) > 0}
          {t(lang, "account.devicesOf", {
            used: digits((a.devicesUsed && a.devicesUsed > 0 ? a.devicesUsed : 1), lang),
            max: digits(a.maxDevices, lang),
          })}
        {:else}
          —
        {/if}
      </span>
    </div>
  </div>

  <button class="btn primary block" onclick={() => { app.error = null; app.view = "invite"; }}>{t(lang, "action.import")}</button>
  {#if app.status.configured}
    <button class="btn quiet block" onclick={logout}>{t(lang, "account.logout")}</button>
  {/if}

  <p class="mono muted ver">{t(lang, "about.version")} <bdi>{app.info.version}</bdi></p>
</section>

<style>
  .page {
    display: flex; flex-direction: column; gap: var(--dot-space-4);
    padding: var(--dot-space-5) var(--dot-space-4) var(--dot-space-4);
    background:
      radial-gradient(70% 40% at 0% 0%, color-mix(in srgb, var(--dot-fg) 4%, transparent), transparent 65%);
  }
  .head { display: flex; flex-direction: column; gap: var(--dot-space-1); }
  .list { display: flex; flex-direction: column; padding: 0; overflow: hidden; }
  .row {
    display: flex; flex-direction: column; gap: 2px;
    padding: var(--dot-space-3) var(--dot-space-4); border-bottom: 1px solid var(--dot-border);
  }
  .row:last-child { border-bottom: 0; }
  .ver { font-size: 12px; }
</style>
