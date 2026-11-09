import { api, toAppError } from "./api";
import { errorText, systemLang } from "./i18n";
import { MAX_SAMPLES, push, rate, type Counters, type Sample } from "./throughput";
import {
  emptyAccount,
  emptySettings,
  emptyStatus,
  type Account,
  type AppError,
  type Info,
  type Lang,
  type Prefs,
  type Relay,
  type Server,
  type Settings,
  type Status,
  type Theme,
} from "./types";

export type Tab = "home" | "account" | "relay" | "settings";
export type View = Tab | "invite";
export type SettingsPage = "app" | "connection" | "dns" | "bypass";

const defaultPrefs: Prefs = { language: "", theme: "system", startAtLogin: false, startMinimized: true };

/** Window state. Everything real lives in the helper; this only mirrors it. */
class App {
  ready = $state(false);
  info = $state<Info>({ os: "darwin", version: "", trayHidden: false });
  prefs = $state<Prefs>(defaultPrefs);
  /** null until the first answer from the helper. */
  helper = $state<boolean | null>(null);
  status = $state<Status>(emptyStatus);
  settings = $state<Settings>(emptySettings);
  servers = $state<Server[]>([]);
  account = $state<Account>(emptyAccount);
  relays = $state<Relay[]>([]);
  samples = $state<Sample[]>([]);
  view = $state<View>("home");
  settingsPage = $state<SettingsPage>("app");
  pickerOpen = $state(false);
  /** Wall clock, ticking once a second so uptime moves. */
  now = $state(Date.now());
  /** A failed action, shown inline where it happened. */
  error = $state<AppError | null>(null);
  notice = $state("");

  #last: Counters | null = null;

  get lang(): Lang {
    return this.prefs.language || "en";
  }

  get dark(): boolean {
    return this.prefs.theme === "dark" || (this.prefs.theme === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  }

  /** Text for the current error, in the current language. */
  get errorMessage(): string {
    return this.error ? errorText(this.lang, this.error.code) : "";
  }

  async init(): Promise<void> {
    this.info = await api.info();
    const p = await api.prefs();
    if (!p.language) {
      p.language = systemLang();
      await api.setPrefs(p).catch(() => {});
    }
    this.prefs = p;
    api.on("prefs", (v) => (this.prefs = v));
    api.on("helper", (up) => {
      this.helper = up;
      if (up) void this.refresh();
      else this.status = emptyStatus;
    });
    api.on("status", (s) => this.#onStatus(s));
    api.on("invite", (r) => {
      if (r.ok) {
        this.notice = "invite.done";
        this.error = null;
        this.view = "home";
        void this.refresh();
      } else {
        this.error = { code: r.code ?? "internal", message: r.message ?? "" };
        this.view = "invite";
      }
    });
    await this.refresh();
    const pending = await api.takeInvite().catch(() => null);
    if (pending && !pending.ok) {
      this.error = { code: pending.code ?? "internal", message: pending.message ?? "" };
      this.view = "invite";
    }
    this.ready = true;
    setInterval(() => (this.now = Date.now()), 1000);
    setInterval(() => void this.#poll(), 1000);
  }

  async refresh(): Promise<void> {
    try {
      this.#onStatus(await api.status());
      this.helper = true;
      this.settings = await api.settings();
      this.servers = await api.servers();
      if (this.status.configured) {
        this.account = await api.account().catch(() => emptyAccount);
        this.relays = await api.relays().catch(() => []);
      }
    } catch (e) {
      const err = toAppError(e);
      // helper_down: nothing listening. version: old system helper vs new app —
      // both need Setup so InstallHelper can replace the root-owned binary.
      if (err.code === "helper_down" || err.code === "version") {
        this.helper = false;
        if (err.code === "version") this.error = err;
      }
    }
  }

  async #poll(): Promise<void> {
    if (!this.helper || document.visibilityState === "hidden") return;
    const live = this.status.state === "connected" || this.status.state === "switching";
    try {
      if (live) this.#onStatus(await api.status());
      if (this.view === "home" || this.pickerOpen) this.servers = await api.servers();
      if (this.view === "account") this.account = await api.account();
      if (this.view === "relay") this.relays = await api.relays();
    } catch {
      // The helper going away arrives as a "helper" event; polling stays quiet.
    }
  }

  #onStatus(s: Status): void {
    const prev = this.status.state;
    this.status = s;
    this.helper = true;
    if (!s.running || s.state === "off") {
      this.#last = null;
      if (this.samples.length) this.samples = [];
    } else {
      const cur: Counters = { up: s.upBytes, down: s.downBytes, at: Date.parse(s.at) || Date.now() };
      const r = rate(this.#last, cur);
      this.#last = cur;
      if (r) this.samples = push(this.samples, r);
    }
    if (prev !== s.state) void api.servers().then((v) => (this.servers = v)).catch(() => {});
    if (s.state !== "failed" && prev === "failed") this.error = null;
  }

  async run(action: () => Promise<void>): Promise<boolean> {
    this.error = null;
    this.notice = "";
    try {
      await action();
      return true;
    } catch (e) {
      this.error = toAppError(e);
      return false;
    }
  }

  connect = () => this.run(() => api.connect());
  disconnect = () => this.run(() => api.disconnect());

  async toggleConnect(): Promise<void> {
    const st = this.status;
    if (st.running && (st.state === "connecting" || st.state === "switching" || st.state === "failed")) {
      await this.disconnect();
      return;
    }
    if (st.running && st.state === "connected") {
      await this.disconnect();
      return;
    }
    await this.connect();
  }

  async saveSettings(patch: Partial<Settings>): Promise<void> {
    const next = { ...this.settings, ...patch };
    if (next.killSwitch) next.fullTunnel = true;
    this.settings = next;
    await this.run(() => api.setSettings(next));
  }

  async savePrefs(patch: Partial<Prefs>): Promise<void> {
    const next = { ...this.prefs, ...patch };
    this.prefs = next;
    await this.run(() => api.setPrefs(next));
  }

  setTheme = (theme: Theme) => this.savePrefs({ theme });
  setLang = (language: Lang) => this.savePrefs({ language });

  async addRelay(idOrURI: string): Promise<boolean> {
    if (!(await this.run(() => api.addRelay(idOrURI)))) return false;
    this.relays = await api.relays();
    return true;
  }

  async removeRelay(id: string): Promise<void> {
    if (await this.run(() => api.removeRelay(id))) this.relays = await api.relays();
  }

  async logout(): Promise<boolean> {
    if (!(await this.run(() => api.logout()))) return false;
    this.account = emptyAccount;
    this.relays = [];
    this.servers = [];
    this.status = { ...emptyStatus, configured: false };
    this.view = "invite";
    this.error = null;
    this.notice = "";
    return true;
  }

  get recentSamples(): Sample[] {
    return this.samples.slice(-MAX_SAMPLES);
  }
}

export const app = new App();
