<script lang="ts">
  import { api } from "../lib/api";
  import { app } from "../lib/app.svelte";
  import { flagEmoji } from "../lib/flag";
  import { formatLatency } from "../lib/format";
  import { t } from "../lib/i18n";
  import { groupNodes } from "../lib/nodes";
  import ConnectButton from "../components/ConnectButton.svelte";
  import Hop from "../components/Hop.svelte";
  import Problem from "../components/Problem.svelte";

  const lang = $derived(app.lang);
  const st = $derived(app.status);
  const connected = $derived(st.state === "connected");
  const busy = $derived(st.state === "connecting" || st.state === "switching");
  const blocked = $derived(st.state === "switching" && st.killSwitch);

  const connectLabel = $derived.by(() => {
    // Same orb cancels an in-flight connect — no separate Cancel control.
    if (busy || connected) return t(lang, "action.disconnect");
    if (st.state === "failed" && st.running) return t(lang, "action.retry");
    return t(lang, "action.connect");
  });

  const routed = $derived(!!st.fullTunnel);
  const ipLabel = $derived(connected || busy ? t(lang, "home.exitIp") : t(lang, "home.publicIp"));
  const ipValue = $derived.by(() => {
    // Never fall back to the direct public IP while "connected" — that is how
    // proxy-only sessions looked like a working VPN with the real address.
    if (connected && routed) return st.exitIp || t(lang, "home.ipChecking");
    if (connected) return "-";
    if (busy) return st.exitIp || t(lang, "home.ipChecking");
    return st.publicIp || t(lang, "home.ipChecking");
  });
  /** Full tunnel up = protected exit. Anything else showing a real IP is exposed. */
  const ipTone = $derived.by((): "safe" | "unsafe" | "warn" | "idle" => {
    if (connected && routed && st.exitIp) return "safe";
    if (connected && !routed) return "warn";
    if (busy) return "warn";
    if (st.publicIp || st.state === "failed" || st.state === "off") return "unsafe";
    return "idle";
  });
  const badgeLabel = $derived.by(() => {
    if (ipTone === "safe") return t(lang, "home.ipSafe");
    if (ipTone === "unsafe") return t(lang, "home.ipUnsafe");
    return t(lang, "home.ipCheckingBadge");
  });

  const nodes = $derived(groupNodes(app.servers));
  const selected = $derived.by(() => {
    const pin = app.settings.pin ?? "";
    if (pin) {
      const hit = nodes.find((n) => n.ids.includes(pin));
      if (hit) return hit;
    }
    return nodes.find((n) => n.active) ?? nodes[0];
  });
  // Country comes from the helper / signed list only — never look it up online.
  const flagCC = $derived(selected?.country || st.country || "");

  async function pick(id: string) {
    await app.saveSettings({ pin: id });
    app.servers = await api.servers();
    app.pickerOpen = false;
  }

  async function pickAuto() {
    await app.saveSettings({ pin: "" });
    app.servers = await api.servers();
    app.pickerOpen = false;
  }
</script>

<section class="home">
  <header class="hero">
    <Hop state={st.state} size={48} dark={app.dark} label={t(lang, `state.${st.state}`)} />
    <h1 class="h2" aria-live="polite">{t(lang, `state.${st.state}`)}</h1>
    {#if connected && st.viaRelay}
      <p class="mono muted via">{t(lang, "home.viaRelay", { name: st.viaRelay })}</p>
    {:else if connected && st.carrier}
      <p class="mono muted via">{st.carrier}{#if st.country || flagCC} · {st.country || flagCC}{/if}</p>
    {/if}
  </header>

  <ConnectButton
    state={st.state}
    label={connectLabel}
    onToggle={() => void app.toggleConnect()}
  />

  {#if blocked}<p class="small warning center">{t(lang, "route.blocked")}</p>{/if}
  {#if connected && !routed}
    <p class="small warning center">{t(lang, "home.proxyOnly")}</p>
  {/if}
  {#if st.state === "failed"}
    <p class="small danger center">{t(lang, "err.failed")}</p>
    {#if st.error}<p class="mono muted err center">{st.error}</p>{/if}
  {:else if st.state === "switching"}
    <p class="small muted center">{t(lang, "err.fallback")}</p>
  {:else if busy && st.error}
    <p class="mono muted err center">{st.error}</p>
  {/if}
  <Problem />

  <div class="ip mono center" aria-live="polite">
    <div class="ip-head">
      <span class="caption muted">{ipLabel}</span>
      {#if ipTone !== "idle"}
        <span class="badge" class:safe={ipTone === "safe"} class:unsafe={ipTone === "unsafe"} class:warn={ipTone === "warn"}>{badgeLabel}</span>
      {/if}
    </div>
    <span class="small ip-addr">{ipValue}</span>
    {#if flagCC && (connected || busy)}
      <span class="caption muted ip-meta">{flagEmoji(flagCC)} {flagCC}</span>
    {/if}
  </div>

  <button class="server card" onclick={() => (app.pickerOpen = true)} disabled={app.servers.length === 0}>
    <span class="flag" aria-hidden="true">{flagEmoji(flagCC) || "·"}</span>
    <span class="name">
      <span class="small">{selected ? selected.name : t(lang, "servers.empty")}</span>
      {#if flagCC}<span class="caption muted">{flagCC}</span>{/if}
    </span>
    <span class="meta mono muted">
      {#if selected?.latencyMs}{formatLatency(selected.latencyMs, lang)}
      {:else if !app.settings.pin}{t(lang, "servers.auto")}
      {:else}-{/if}
    </span>
  </button>
</section>

{#if app.pickerOpen}
  <div class="sheet" role="dialog" aria-label={t(lang, "home.pickServer")}>
    <button class="backdrop" aria-label={t(lang, "action.back")} onclick={() => (app.pickerOpen = false)}></button>
    <div class="panel card">
      <header class="sheet-head">
        <h2 class="title">{t(lang, "home.pickServer")}</h2>
        <button class="btn quiet compact" onclick={() => (app.pickerOpen = false)}>{t(lang, "action.back")}</button>
      </header>
      <ul class="list" role="radiogroup" aria-label={t(lang, "home.pickServer")}>
        <li>
          <button role="radio" aria-checked={!app.settings.pin} class="row" onclick={pickAuto}>
            <span class="mark" class:sel={!app.settings.pin}></span>
            <span class="flag auto" aria-hidden="true">◎</span>
            <span class="name">
              <span class="small">{t(lang, "servers.auto")}</span>
              <span class="caption muted">{t(lang, "servers.autoHint")}</span>
            </span>
          </button>
        </li>
        {#each nodes as n (n.name)}
          <li>
            <button role="radio" aria-checked={!!app.settings.pin && n.ids.includes(app.settings.pin)} class="row" onclick={() => pick(n.id)}>
              <span class="mark" class:sel={!!app.settings.pin && n.ids.includes(app.settings.pin)}></span>
              <span class="flag" aria-hidden="true">{flagEmoji(n.country) || "·"}</span>
              <span class="name">
                <span class="small">{n.name}</span>
                {#if n.active}<span class="caption live"><span class="dot" aria-hidden="true"></span>{t(lang, "servers.active")}</span>
                {:else if n.country}<span class="caption muted">{n.country}</span>{/if}
              </span>
              <span class="meta mono muted">{n.latencyMs ? formatLatency(n.latencyMs, lang) : "-"}</span>
            </button>
          </li>
        {/each}
      </ul>
    </div>
  </div>
{/if}

<style>
  .home {
    display: flex; flex-direction: column; align-items: center; gap: var(--dot-space-5);
    padding: var(--dot-space-6) var(--dot-space-4) var(--dot-space-5);
    background:
      radial-gradient(80% 50% at 50% -10%, color-mix(in srgb, var(--dot-fg) 5%, transparent), transparent 70%);
  }
  .home > :global(.problem),
  .home > .server,
  .home > .ip { align-self: stretch; width: 100%; }
  .hero { display: flex; flex-direction: column; align-items: center; gap: var(--dot-space-2); text-align: center; }
  .via { margin: 0; font-size: 12px; }
  .center { text-align: center; }
  .err { overflow-wrap: anywhere; font-size: 12px; }
  .ip {
    display: flex; flex-direction: column; align-items: center; gap: 4px; margin: 0;
  }
  .ip-head { display: flex; align-items: center; justify-content: center; gap: var(--dot-space-2); }
  .ip-addr { letter-spacing: 0.01em; }
  .ip-meta { max-width: 22rem; text-align: center; overflow-wrap: anywhere; }
  .badge {
    display: inline-flex; align-items: center; min-height: 20px; padding: 0 8px;
    border-radius: var(--dot-radius-pill); font: 600 11px / 1 var(--dot-font-sans);
    letter-spacing: 0.02em; text-transform: uppercase;
  }
  .badge.unsafe {
    color: #fff; background: var(--dot-danger);
  }
  .badge.warn {
    color: #fff; background: var(--dot-warning);
  }
  .badge.safe {
    color: #1a2e05; background: var(--dot-signal);
  }
  .server {
    display: flex; align-items: center; gap: var(--dot-space-3); width: 100%;
    min-height: 60px; padding: var(--dot-space-3) var(--dot-space-4);
    background: var(--dot-surface); border: 1px solid var(--dot-border); text-align: start; cursor: pointer;
    transition: background var(--dot-dur-fast) var(--dot-ease), border-color var(--dot-dur-fast) var(--dot-ease);
  }
  .server:hover:not(:disabled) { background: var(--dot-surface-2); }
  .server:disabled { opacity: 0.5; cursor: default; }
  .flag { flex: none; width: 1.75em; text-align: center; font-size: 20px; line-height: 1; }
  .flag.auto { font-size: 14px; color: var(--dot-fg-muted); }
  .name { flex: 1; display: flex; flex-direction: column; min-width: 0; gap: 1px; }
  .meta { flex: none; font-size: 12px; min-width: 4.5em; text-align: end; }
  .sheet { position: fixed; inset: 0; z-index: 20; display: flex; align-items: flex-end; justify-content: center; }
  .backdrop { position: absolute; inset: 0; background: rgba(0,0,0,0.4); border: 0; backdrop-filter: blur(2px); }
  .panel {
    position: relative; width: min(680px, 100%); max-height: 72vh; overflow: auto;
    border-radius: var(--dot-radius-lg) var(--dot-radius-lg) 0 0; padding: 0;
    box-shadow: var(--dot-shadow-menu);
    animation: rise 220ms var(--dot-ease);
  }
  @keyframes rise {
    from { transform: translateY(12px); opacity: 0.6; }
    to { transform: none; opacity: 1; }
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
  li + li { border-top: 1px solid var(--dot-border); }
  .row {
    display: flex; align-items: center; gap: var(--dot-space-3); width: 100%; min-height: 56px;
    padding: var(--dot-space-2) var(--dot-space-4); background: none; border: 0; text-align: start;
  }
  .row:hover { background: var(--dot-surface-2); }
  .mark { flex: none; width: 16px; height: 16px; border-radius: 50%; border: 1.5px solid var(--dot-off); }
  .mark.sel { border-color: var(--dot-fg); box-shadow: inset 0 0 0 3px var(--dot-bg); background: var(--dot-fg); }
  .live { display: inline-flex; align-items: center; gap: var(--dot-space-2); color: var(--dot-signal-fg); }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--dot-signal); box-shadow: 0 0 0 2px var(--dot-bg); }
  @media (prefers-reduced-motion: reduce) {
    .panel { animation: none; }
  }
</style>
