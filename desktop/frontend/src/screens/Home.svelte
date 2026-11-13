<script lang="ts">
  import { api } from "../lib/api";
  import { app } from "../lib/app.svelte";
  import { formatDuration, formatLatency } from "../lib/format";
  import { t } from "../lib/i18n";
  import { groupNodes } from "../lib/nodes";
  import AdSlot from "../components/AdSlot.svelte";
  import Flag from "../components/Flag.svelte";
  import ConnectButton from "../components/ConnectButton.svelte";
  import Hop from "../components/Hop.svelte";
  import Icon from "../components/Icon.svelte";
  import Problem from "../components/Problem.svelte";
  import type { Ad } from "../lib/types";

  const auto = $derived(!(app.settings.pin ?? ""));

  const lang = $derived(app.lang);
  const st = $derived(app.status);
  const connected = $derived(st.state === "connected");
  const busy = $derived(st.state === "connecting" || st.state === "switching");
  const blocked = $derived(st.state === "switching" && st.killSwitch);
  const relayBadge = $derived(
    t(lang, st.viaRelayTier === "community" ? "home.communityRelay" : "home.relay", {
      carrier: st.carrier || "relay",
    }),
  );

  const connectLabel = $derived.by(() => {
    // Same orb cancels an in-flight connect — no separate Cancel control.
    if (busy || connected) return t(lang, "action.disconnect");
    if (st.state === "failed" && st.running) return t(lang, "action.retry");
    return t(lang, "action.connect");
  });

  const uptime = $derived.by(() => {
    if (!connected || !st.since) return "";
    const ms = app.now - Date.parse(st.since);
    if (!Number.isFinite(ms) || ms < 0) return "";
    return formatDuration(ms / 1000, lang);
  });
  const sessionPing = $derived(connected && st.latencyMs ? formatLatency(st.latencyMs, lang) : "");

  let probing = $state(false);
  let probedOnce = $state(false);

  const routed = $derived(!!st.fullTunnel);
  // Label stays "IP"; Safe/Unsafe badge already says whether the address is protected.
  const ipLabel = $derived(t(lang, "home.publicIp"));
  const ipValue = $derived.by(() => {
    // Never fall back to the direct public IP while "connected" — that is how
    // proxy-only sessions looked like a working VPN with the real address.
    if (connected && routed) return st.exitIp || t(lang, "home.ipChecking");
    if (connected) return "-";
    if (busy) return st.exitIp || t(lang, "home.ipChecking");
    if (st.publicIp) return st.publicIp;
    // Offline / failed: do not spin on "checking" forever when the probe is empty.
    if (st.state === "off" || st.state === "failed") return "-";
    return t(lang, "home.ipChecking");
  });
  /** Full tunnel up = protected exit. Anything else showing a real IP is exposed. */
  const ipTone = $derived.by((): "safe" | "unsafe" | "warn" | "idle" => {
    if (connected && routed && st.exitIp) return "safe";
    if (connected && !routed) return "warn";
    if (busy) return "warn";
    if (st.publicIp) return "unsafe";
    if (st.state === "failed" || st.state === "off") return "idle";
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

  async function openPicker() {
    app.pickerOpen = true;
    const needProbe = !probedOnce && !app.servers.some((s) => (s.latencyMs ?? 0) > 0);
    if (!needProbe || probing) return;
    probing = true;
    try {
      app.servers = await api.probeServers();
      probedOnce = true;
    } catch {
      // keep list without latencies
    } finally {
      probing = false;
    }
  }

  let homeAd = $state<Ad | null>(null);
  // Resolve control relay id for /v1/ads — never pass viaRelay display name.
  const homeRelayId = $derived(
    connected && st.viaRelay
      ? st.viaRelayId || app.relays.find((r) => r.name === st.viaRelay)?.id || ""
      : "",
  );
  $effect(() => {
    if (!st.configured) {
      homeAd = null;
      return;
    }
    const relayId = homeRelayId;
    void (async () => {
      try {
        const ads = await api.ads("home", relayId);
        homeAd = ads[0] ?? null;
      } catch {
        homeAd = null;
      }
    })();
  });
</script>

<section class="home">
  <header class="hero">
    <div class="brand">
      <Hop state={st.state} size={40} dark={app.dark} label={t(lang, `state.${st.state}`)} />
      <span class="logotype">{t(lang, "app.name")}</span>
    </div>
    <h1 class="h2 status" aria-live="polite">{t(lang, `state.${st.state}`)}</h1>
    {#if connected && st.viaRelay}
      <span class="badge relay">{relayBadge}</span>
      <p class="mono muted via">{t(lang, "home.viaRelay", { name: st.viaRelay })}</p>
    {/if}
  </header>

  {#if uptime}
    <p class="mono muted uptime" aria-live="polite">{uptime}</p>
  {/if}

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
    <span class="caption muted">{ipLabel}</span>
    <div class="ip-row">
      <span class="small ip-addr">{ipValue}</span>
      {#if ipTone !== "idle"}
        <span class="badge" class:safe={ipTone === "safe"} class:unsafe={ipTone === "unsafe"} class:warn={ipTone === "warn"}>{badgeLabel}</span>
      {/if}
    </div>
    {#if sessionPing || (connected && st.carrier)}
      <div class="ip-row ping-row">
        {#if sessionPing}
          <span class="caption muted">{t(lang, "home.ping")} · {sessionPing}</span>
        {/if}
        {#if connected && st.carrier}
          <span class="badge safe proto" title={st.carrier}>{st.carrier}</span>
        {/if}
      </div>
    {/if}
  </div>

  <button class="server card" class:auto-on={auto} onclick={() => void openPicker()} disabled={app.servers.length === 0}>
    {#if auto}
      <span class="flag auto" aria-hidden="true"><Icon name="route" size={22} /></span>
      <span class="name">
        <span class="small">{t(lang, "servers.auto")}</span>
        {#if selected && (connected || busy)}
          <span class="caption muted">{selected.name}{#if flagCC} · {flagCC}{/if}</span>
        {:else}
          <span class="caption muted">{t(lang, "servers.autoHint")}</span>
        {/if}
      </span>
      <span class="meta mono muted">{selected?.latencyMs ? formatLatency(selected.latencyMs, lang) : "·"}</span>
    {:else}
      <Flag cc={flagCC} />
      <span class="name">
        <span class="small">{selected ? selected.name : t(lang, "servers.empty")}</span>
        {#if flagCC}<span class="caption muted">{flagCC}</span>{/if}
      </span>
      <span class="meta mono muted">{selected?.latencyMs ? formatLatency(selected.latencyMs, lang) : "-"}</span>
    {/if}
  </button>

  <div class="ad-dock">
    <AdSlot ad={homeAd} />
  </div>
</section>

{#if app.pickerOpen}
  <div class="sheet" role="dialog" aria-label={t(lang, "home.pickServer")}>
    <button class="backdrop" aria-label={t(lang, "action.back")} onclick={() => (app.pickerOpen = false)}></button>
    <div class="panel card">
      <header class="sheet-head">
        <h2 class="title">{t(lang, "home.pickServer")}</h2>
        <button class="btn quiet compact" onclick={() => (app.pickerOpen = false)}>{t(lang, "action.back")}</button>
      </header>
      {#if probing}
        <p class="caption muted probe">{t(lang, "servers.probing")}</p>
      {/if}
      <ul class="list" role="radiogroup" aria-label={t(lang, "home.pickServer")}>
        <li>
          <button role="radio" aria-checked={auto} class="row" class:picked={auto} onclick={pickAuto}>
            <span class="mark" class:sel={auto}></span>
            <span class="flag auto" aria-hidden="true"><Icon name="route" size={20} /></span>
            <span class="name">
              <span class="small">{t(lang, "servers.auto")}</span>
              <span class="caption muted">{t(lang, "servers.autoHint")}</span>
            </span>
            {#if auto}<span class="meta caption live">{t(lang, "servers.pinned")}</span>{/if}
          </button>
        </li>
        {#each nodes as n (n.name)}
          <li>
            <button role="radio" aria-checked={!!app.settings.pin && n.ids.includes(app.settings.pin)} class="row" class:picked={!!app.settings.pin && n.ids.includes(app.settings.pin)} onclick={() => pick(n.id)}>
              <span class="mark" class:sel={!!app.settings.pin && n.ids.includes(app.settings.pin)}></span>
              <Flag cc={n.country} />
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
    min-height: 100%;
    padding: var(--dot-space-6) var(--dot-space-4) var(--dot-space-4);
    background:
      radial-gradient(80% 50% at 50% -10%, color-mix(in srgb, var(--dot-fg) 5%, transparent), transparent 70%);
  }
  .home > :global(.problem),
  .home > .server,
  .home > .ip,
  .home > .ad-dock { align-self: stretch; width: 100%; }
  .ad-dock { margin-top: auto; padding-top: var(--dot-space-3); }
  .hero { display: flex; flex-direction: column; align-items: center; gap: var(--dot-space-2); text-align: center; }
  .brand {
    display: flex; align-items: center; justify-content: center; gap: var(--dot-space-3);
  }
  .logotype {
    font: 600 22px / 1.1 var(--dot-font-sans);
    letter-spacing: -0.03em;
    color: var(--dot-fg);
  }
  .status { margin: 0; }
  .via { margin: 0; font-size: 12px; }
  .uptime { margin: 0; font-size: 18px; letter-spacing: 0.04em; font-variant-numeric: tabular-nums; }
  .probe { padding: var(--dot-space-2) var(--dot-space-4); margin: 0; }
  .center { text-align: center; }
  .err { overflow-wrap: anywhere; font-size: 12px; }
  .ip {
    display: flex; flex-direction: column; align-items: center; gap: 4px; margin: 0;
  }
  .ip-row { display: flex; align-items: center; justify-content: center; gap: var(--dot-space-2); flex-wrap: wrap; }
  .ping-row { margin-top: 2px; }
  .ip-addr { letter-spacing: 0.01em; }
  .badge.proto { text-transform: none; letter-spacing: 0.01em; font-family: var(--dot-font-mono); }
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
  .badge.relay {
    color: var(--dot-fg); background: var(--dot-surface-2); border: 1px solid var(--dot-border);
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
  .flag.auto {
    display: inline-flex; align-items: center; justify-content: center;
    color: var(--dot-fg-muted);
  }
  .server.auto-on .flag.auto { color: var(--dot-fg); }
  .row.picked { background: color-mix(in srgb, var(--dot-fg) 4%, transparent); }
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
