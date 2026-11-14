<script lang="ts">
  import { onMount } from "svelte";
  import { app, type Tab } from "./lib/app.svelte";
  import { dir, t } from "./lib/i18n";
  import Icon from "./components/Icon.svelte";
  import Account from "./screens/Account.svelte";
  import Home from "./screens/Home.svelte";
  import Invite from "./screens/Invite.svelte";
  import Relay from "./screens/Relay.svelte";
  import Settings from "./screens/Settings.svelte";
  import Setup from "./screens/Setup.svelte";

  onMount(() => void app.init());

  const lang = $derived(app.lang);
  // Until the helper is installed and an invite imported there is nothing to
  // navigate: show the one action that gets the user there.
  const firstRun = $derived(app.ready && (app.helper === false || !app.status.configured) && app.view !== "settings" && app.view !== "invite");

  $effect(() => {
    document.documentElement.lang = lang;
    document.documentElement.dir = dir(lang);
    document.documentElement.dataset.theme = app.prefs.theme === "system" ? "" : app.prefs.theme;
    if (!document.documentElement.dataset.theme) delete document.documentElement.dataset.theme;
  });

  const tabs: { id: Tab; icon: string; key: string }[] = [
    { id: "home", icon: "connect", key: "tab.home" },
    { id: "account", icon: "invite", key: "tab.account" },
    { id: "relay", icon: "through", key: "tab.relay" },
    { id: "settings", icon: "settings", key: "tab.settings" },
  ];
</script>

{#if app.ready}
  <div class="shell">
    <main>
      {#if firstRun}
        <Setup />
      {:else if app.view === "home"}
        <Home />
      {:else if app.view === "account"}
        <Account />
      {:else if app.view === "relay"}
        <Relay />
      {:else if app.view === "invite"}
        <Invite />
      {:else}
        <Settings />
      {/if}
    </main>
    {#if app.pendingApply}
      <p class="toast small" role="status">{t(lang, "settings.applyNext")}</p>
    {/if}
    {#if !firstRun}
      <nav aria-label="Dot VPN">
        {#each tabs as tab}
          <button class="tab" aria-current={app.view === tab.id ? "page" : undefined} onclick={() => { app.error = null; app.notice = ""; app.view = tab.id; }}>
            <Icon name={tab.icon} size={22} />
            <span class="caption">{t(lang, tab.key)}</span>
          </button>
        {/each}
      </nav>
    {/if}
  </div>
{/if}

<style>
  .shell { height: 100%; display: flex; flex-direction: column; max-width: 680px; margin: 0 auto; }
  main { flex: 1; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; user-select: none; }
  main > :global(section) { flex: 1; }
  .toast {
    margin: 0 var(--dot-space-4) var(--dot-space-3); padding: var(--dot-space-3) var(--dot-space-4);
    background: var(--dot-surface-2); border: 1px solid var(--dot-border); border-radius: var(--dot-radius-md);
  }
  nav {
    display: flex; border-top: 1px solid var(--dot-border);
    background: color-mix(in srgb, var(--dot-bg) 92%, transparent);
    backdrop-filter: blur(12px);
    padding-bottom: env(safe-area-inset-bottom);
  }
  .tab {
    flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 2px;
    min-height: 56px; padding: var(--dot-space-2) 0; background: none; border: 0; color: var(--dot-fg-muted);
    transition: color var(--dot-dur-fast) var(--dot-ease);
  }
  .tab[aria-current="page"] { color: var(--dot-fg); }
  .tab:hover { color: var(--dot-fg); }
</style>
