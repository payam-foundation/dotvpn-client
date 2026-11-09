<script lang="ts">
  import { api } from "../lib/api";
  import { app, type SettingsPage } from "../lib/app.svelte";
  import { t } from "../lib/i18n";
  import { parseEntries } from "../lib/validate";
  import type { Lang, Theme } from "../lib/types";
  import Problem from "../components/Problem.svelte";
  import Segmented from "../components/Segmented.svelte";
  import Toggle from "../components/Toggle.svelte";

  const lang = $derived(app.lang);
  const s = $derived(app.settings);
  const page = $derived(app.settingsPage);
  let dns = $state("");
  let routeDraft = $state("");
  let routeErr = $state("");
  let flash = $state("");

  const carriers = ["h3", "h2", "cdnws", "webtunnel", "snowflake", "reverse", "dns"] as const;

  const presetMeta: Record<string, string> = {
    cloudflare: "1.1.1.1",
    google: "8.8.8.8",
    quad9: "9.9.9.9",
  };

  $effect(() => {
    dns = app.settings.dns;
  });

  async function unblock() {
    if (await app.run(() => api.unblock())) flash = "settings.unblocked";
  }

  async function setPreset(preset: string) {
    const patch: Parameters<typeof app.saveSettings>[0] = { dnsPreset: preset };
    if (preset === "custom") patch.dns = dns || s.dns;
    await app.saveSettings(patch);
  }

  function toggleCarrier(kind: string) {
    const cur = new Set(s.carriers ?? []);
    if (cur.size === 0) {
      for (const k of carriers) if (k !== kind) cur.add(k);
    } else if (cur.has(kind)) {
      cur.delete(kind);
    } else {
      cur.add(kind);
    }
    const next = [...cur];
    void app.saveSettings({ carriers: next.length === carriers.length ? [] : next });
  }

  function carrierOn(kind: string): boolean {
    const c = s.carriers ?? [];
    return c.length === 0 || c.includes(kind);
  }

  async function addRoute() {
    const { ok, bad } = parseEntries(routeDraft);
    if (ok.length === 0) {
      routeErr = "routing.invalid";
      return;
    }
    routeErr = bad.length ? "routing.partial" : "";
    routeDraft = bad.join("\n");
    const domains = new Set(s.directDomains);
    const cidrs = new Set(s.directCidrs);
    for (const p of ok) {
      if (p.kind === "domain") domains.add(p.value);
      else cidrs.add(p.value);
    }
    await app.saveSettings({ directDomains: [...domains], directCidrs: [...cidrs] });
  }

  async function removeRoute(kind: "domain" | "cidr", value: string) {
    if (kind === "domain") {
      await app.saveSettings({ directDomains: s.directDomains.filter((x) => x !== value) });
    } else {
      await app.saveSettings({ directCidrs: s.directCidrs.filter((x) => x !== value) });
    }
  }

  function presetLabel(p: string): string {
    const name = t(lang, `dns.${p}`);
    if (p === "system") {
      const ip = s.systemDns?.trim();
      return ip ? `${name} (${ip})` : name;
    }
    if (p === "custom") return name;
    const ip = presetMeta[p];
    return ip ? `${name} (${ip})` : name;
  }

  const themes = $derived<{ value: Theme; label: string }[]>(
    (["system", "light", "dark"] as const).map((v) => ({ value: v, label: t(lang, `theme.${v}`) })),
  );
  const langs: { value: Lang; label: string }[] = [
    { value: "en", label: "English" },
    { value: "fa", label: "فارسی" },
  ];
  const pages: { id: SettingsPage; key: string }[] = [
    { id: "app", key: "settings.app" },
    { id: "connection", key: "settings.connection" },
    { id: "dns", key: "settings.dnsPage" },
    { id: "bypass", key: "settings.bypass" },
  ];
  const presets = ["system", "cloudflare", "google", "quad9", "custom"] as const;
</script>

<section class="page">
  <h1 class="h3">{t(lang, "settings.title")}</h1>
  <div class="subnav" role="tablist" aria-label={t(lang, "settings.title")}>
    {#each pages as p}
      <button role="tab" aria-selected={page === p.id} class="sub" onclick={() => (app.settingsPage = p.id)}>{t(lang, p.key)}</button>
    {/each}
  </div>
  <Problem />
  {#if flash}<p class="small" role="status">{t(lang, flash)}</p>{/if}

  {#if page === "app"}
    <div class="card">
      <Toggle checked={app.prefs.startAtLogin} label={t(lang, "settings.startAtLogin")} onchange={(v) => app.savePrefs({ startAtLogin: v })} />
      <hr />
      <Toggle checked={app.prefs.startMinimized} label={t(lang, "settings.startMinimized")} disabled={app.info.trayHidden} onchange={(v) => app.savePrefs({ startMinimized: v })} />
      <hr />
      <div class="pick">
        <span class="small">{t(lang, "settings.language")}</span>
        <Segmented value={lang} options={langs} label={t(lang, "settings.language")} onchange={(v) => app.setLang(v)} />
      </div>
      <hr />
      <div class="pick">
        <span class="small">{t(lang, "settings.theme")}</span>
        <Segmented value={app.prefs.theme} options={themes} label={t(lang, "settings.theme")} onchange={(v) => app.setTheme(v)} />
      </div>
    </div>
    {#if app.info.trayHidden}<p class="caption warning">{t(lang, "settings.noTray")}</p>{/if}
    <div class="card actions">
      <button class="btn quiet block" onclick={unblock} disabled={app.status.running}>{t(lang, "settings.unblock")}</button>
      <p class="caption muted">{t(lang, "settings.unblockHint")}</p>
    </div>
  {:else if page === "connection"}
    <div class="card">
      <Toggle checked={s.fullTunnel} label={t(lang, "settings.fullTunnel")} hint={t(lang, "settings.fullTunnelHint")} disabled={s.killSwitch} onchange={(v) => app.saveSettings({ fullTunnel: v })} />
      <hr />
      <Toggle checked={s.killSwitch} label={t(lang, "settings.killSwitch")} hint={t(lang, "settings.killSwitchHint")} onchange={(v) => app.saveSettings({ killSwitch: v })} />
    </div>
    <div class="group">
      <h2 class="caption muted">{t(lang, "settings.carriers")}</h2>
      <p class="caption muted">{t(lang, "settings.carriersHint")}</p>
      <div class="card chips">
        {#each carriers as k}
          <button class="chip" class:on={carrierOn(k)} onclick={() => toggleCarrier(k)}>{k}</button>
        {/each}
      </div>
    </div>
    <p class="caption muted">{t(lang, "settings.applyNext")}</p>
  {:else if page === "dns"}
    <div class="card">
      <label class="dns">
        <span class="small">{t(lang, "settings.dnsPreset")}</span>
        <select
          class="field select"
          aria-label={t(lang, "settings.dnsPreset")}
          value={s.dnsPreset || "cloudflare"}
          onchange={(e) => void setPreset((e.currentTarget as HTMLSelectElement).value)}
        >
          {#each presets as p}
            <option value={p}>{presetLabel(p)}</option>
          {/each}
        </select>
      </label>
      {#if (s.dnsPreset || "cloudflare") === "custom"}
        <hr />
        <label class="dns">
          <span class="small">{t(lang, "settings.dns")}</span>
          <input
            class="field mono" dir="ltr" spellcheck="false" autocomplete="off" bind:value={dns}
            onchange={() => dns !== s.dns && app.saveSettings({ dns, dnsPreset: "custom" })}
          />
          <span class="caption muted">{t(lang, "settings.dnsHint")}</span>
        </label>
      {/if}
      <hr />
      <Toggle checked={s.doh} label={t(lang, "settings.doh")} hint={t(lang, "settings.dohHint")} onchange={(v) => app.saveSettings({ doh: v })} />
    </div>
    <p class="caption muted">{t(lang, "settings.applyNext")}</p>
  {:else}
    <div class="group">
      <h2 class="caption muted">{t(lang, "routing.title")}</h2>
      <p class="caption muted">{t(lang, "routing.body")}</p>
      <label class="add">
        <span class="caption muted">{t(lang, "routing.add")}</span>
        <div class="row-input">
          <input
            class="field mono"
            dir="ltr"
            spellcheck="false"
            autocomplete="off"
            aria-label={t(lang, "routing.add")}
            placeholder={t(lang, "routing.placeholder")}
            bind:value={routeDraft}
            onkeydown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                void addRoute();
              }
            }}
            onpaste={(e) => {
              const text = e.clipboardData?.getData("text") ?? "";
              if (!/[\n\r,;]/.test(text)) return;
              e.preventDefault();
              routeDraft = text;
              void addRoute();
            }}
          />
          <button class="btn" onclick={addRoute}>{t(lang, "action.add")}</button>
        </div>
      </label>
      {#if routeErr}<p class="small danger">{t(lang, routeErr)}</p>{/if}
      {#if s.directDomains.length === 0 && s.directCidrs.length === 0}
        <p class="small muted">{t(lang, "routing.empty")}</p>
      {:else}
        <ul class="card list">
          {#each s.directDomains as d}
            <li class="route">
              <span class="mono small">{d}</span>
              <button class="btn quiet" aria-label="{t(lang, 'action.remove')} {d}" onclick={() => removeRoute("domain", d)}>{t(lang, "action.remove")}</button>
            </li>
          {/each}
          {#each s.directCidrs as c}
            <li class="route">
              <span class="mono small">{c}</span>
              <button class="btn quiet" aria-label="{t(lang, 'action.remove')} {c}" onclick={() => removeRoute("cidr", c)}>{t(lang, "action.remove")}</button>
            </li>
          {/each}
        </ul>
      {/if}
      <p class="caption muted">{t(lang, "settings.applyNext")}</p>
    </div>
  {/if}
</section>

<style>
  .page {
    display: flex; flex-direction: column; gap: var(--dot-space-4);
    padding: var(--dot-space-5) var(--dot-space-4) var(--dot-space-4);
    background:
      radial-gradient(70% 40% at 100% 0%, color-mix(in srgb, var(--dot-fg) 4%, transparent), transparent 65%);
  }
  .subnav { display: flex; gap: var(--dot-space-2); flex-wrap: wrap; }
  .sub {
    padding: 6px var(--dot-space-3); border: 1px solid var(--dot-border); border-radius: var(--dot-radius-pill);
    background: none; color: var(--dot-fg-muted); font: inherit;
    transition: color var(--dot-dur-fast) var(--dot-ease), border-color var(--dot-dur-fast) var(--dot-ease), background var(--dot-dur-fast) var(--dot-ease);
  }
  .sub[aria-selected="true"] { color: var(--dot-fg); border-color: var(--dot-fg); background: color-mix(in srgb, var(--dot-fg) 4%, transparent); }
  hr { margin: 0; border: 0; border-top: 1px solid var(--dot-border); }
  .dns, .pick, .add { display: flex; flex-direction: column; gap: var(--dot-space-2); padding: var(--dot-space-3) var(--dot-space-4); }
  .actions { padding: var(--dot-space-4); }
  .group { display: flex; flex-direction: column; gap: var(--dot-space-2); }
  .chips { display: flex; flex-wrap: wrap; gap: var(--dot-space-2); padding: var(--dot-space-3) var(--dot-space-4); }
  .chip {
    padding: 4px 10px; border: 1px solid var(--dot-border); border-radius: var(--dot-radius-pill);
    background: none; color: var(--dot-fg-muted); font: inherit; font-family: var(--dot-font-mono); font-size: 12px;
  }
  .chip.on { color: var(--dot-fg); border-color: var(--dot-fg); }
  .select {
    appearance: none;
    background-image: linear-gradient(45deg, transparent 50%, var(--dot-fg-muted) 50%), linear-gradient(135deg, var(--dot-fg-muted) 50%, transparent 50%);
    background-position: calc(100% - 18px) calc(50% - 2px), calc(100% - 12px) calc(50% - 2px);
    background-size: 6px 6px, 6px 6px;
    background-repeat: no-repeat;
    padding-inline-end: 36px;
  }
  :global([dir="rtl"]) .select {
    background-position: 12px calc(50% - 2px), 18px calc(50% - 2px);
    padding-inline-end: var(--dot-space-3);
    padding-inline-start: 36px;
  }
  .row-input { display: flex; gap: var(--dot-space-2); }
  .row-input .field { flex: 1; }
  .list { list-style: none; margin: 0; padding: 0; }
  .route {
    display: flex; align-items: center; justify-content: space-between; gap: var(--dot-space-3);
    min-height: 48px; padding: var(--dot-space-2) var(--dot-space-4); border-top: 1px solid var(--dot-border);
  }
  .route:first-child { border-top: 0; }
</style>
