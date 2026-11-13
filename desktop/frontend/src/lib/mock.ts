// In-memory stand-in for the desktop bindings, used only by `vite --mode mock`
// so Playwright can drive every screen without a helper. Never bundled into
// the real app: api.ts imports it behind VITE_MOCK.
import type { Api, EventMap } from "./api";
import type { Account, AppError, Info, InviteResult, Prefs, Relay, Server, Settings, State, Status } from "./types";

const q = new URLSearchParams(location.search);

const startState = (q.get("state") ?? "off") as State;
let helper = q.get("helper") !== "down";
let configured = q.get("configured") !== "no";
const frozen = q.has("freeze");

let settings: Settings = {
  fullTunnel: true,
  killSwitch: q.has("killswitch"),
  dns: "1.1.1.1:53",
  dnsPreset: "cloudflare",
  doh: false,
  systemDns: "192.168.1.1",
  useRelays: q.has("viarelay"),
  carriers: [],
  directDomains: q.has("routes") ? ["bank.ir", "*.local.example"] : [],
  directCidrs: q.has("routes") ? ["10.0.0.0/8", "192.168.0.0/16"] : [],
};

let prefs: Prefs = {
  language: (q.get("lang") as Prefs["language"]) ?? "en",
  theme: (q.get("theme") as Prefs["theme"]) ?? "system",
  startAtLogin: false,
  startMinimized: true,
};

const info: Info = { os: "darwin", version: "dev", trayHidden: q.has("notray") };

let state: State = startState;
let since = Date.now() - 754_000;
let error = state === "failed" ? "dial h3: timeout" : "";
let t0 = Date.now();

let account: Account = {
  name: "Davod",
  quotaLimit: 50 * 1024 ** 3,
  quotaUsed: 18 * 1024 ** 3,
  quotaRemaining: 32 * 1024 ** 3,
  quotaReset: new Date(Date.UTC(new Date().getUTCFullYear(), new Date().getUTCMonth() + 1, 1)).toISOString(),
  expires: Math.floor(Date.now() / 1000) + 86400 * 40,
  maxDevices: 5,
  devicesUsed: 1,
  mbps: 100,
  disabled: false,
};

let relays: Relay[] = [
  { id: "system-de", name: "Relay DE", country: "DE", tier: "system", health: "ok", public: true, optedIn: false },
  { id: "community-1", name: "Friends NL", country: "NL", tier: "community", health: "degraded", public: true, optedIn: true },
  { id: "private-1", name: "Home Relay", country: "IR", tier: "community", health: "ok", public: false, optedIn: true, invite: "dvr://private" },
];

const servers = (): Server[] => [
  { id: "a", name: "Frankfurt", country: "DE", carrier: "h3", active: (state === "connected" && !settings.pin) || settings.pin === "a", latencyMs: 42 },
  { id: "b", name: "Frankfurt", country: "DE", carrier: "cdnws", active: settings.pin === "b", latencyMs: 118 },
  { id: "c", name: "Istanbul", country: "TR", carrier: "h3", active: false, error: "timeout", cooldownMs: 24_000, latencyMs: 91 },
];

function status(): Status {
  const live = state === "connected" || state === "switching";
  const secs = (Date.now() - t0) / 1000;
  return {
    state: helper ? state : "off",
    running: state !== "off",
    error: error || undefined,
    carrier: state === "connected" ? "h3" : undefined,
    server: state === "connected" ? "Frankfurt" : undefined,
    country: state === "connected" ? "DE" : undefined,
    viaRelay: state === "connected" && q.has("viarelay") ? "Relay DE" : undefined,
    viaRelayTier: state === "connected" && q.has("viarelay") ? "curated" : undefined,
    viaRelayId: state === "connected" && q.has("viarelay") ? "relay-de" : undefined,
    latencyMs: state === "connected" ? 42 : undefined,
    since: state === "connected" ? new Date(since).toISOString() : undefined,
    exitIp: state === "connected" ? "203.0.113.10" : undefined,
    publicIp: "198.51.100.20",
    upBytes: live ? Math.round(secs * 18_000 + 1_200_000) : 0,
    downBytes: live ? Math.round(secs * (240_000 + 90_000 * Math.sin(secs))) + 48_000_000 : 0,
    fullTunnel: settings.fullTunnel,
    killSwitch: settings.killSwitch,
    configured,
    at: new Date().toISOString(),
  };
}

const listeners: { [K in keyof EventMap]: Set<(d: EventMap[K]) => void> } = {
  status: new Set(),
  helper: new Set(),
  invite: new Set(),
  prefs: new Set(),
};
const emit = <K extends keyof EventMap>(name: K, data: EventMap[K]) =>
  (listeners[name] as Set<(d: EventMap[K]) => void>).forEach((cb) => cb(data));

const fail = (code: string, message: string): never => {
  const e = new Error(message) as Error & { cause: AppError };
  e.cause = { code, message };
  throw e;
};

const settle = (s: State, ms: number) => {
  if (frozen) return;
  setTimeout(() => {
    state = s;
    if (s === "connected") {
      since = Date.now();
      t0 = Date.now();
    }
    emit("status", status());
  }, ms);
};

export const mockApi: Api = {
  info: async () => info,
  status: async () => (helper ? status() : fail("helper_down", "cannot reach the helper")),
  connect: async () => {
    if (!configured) fail("not_configured", "no invite");
    state = "connecting";
    error = "";
    emit("status", status());
    settle("connected", 900);
  },
  disconnect: async () => {
    state = "off";
    error = "";
    emit("status", status());
  },
  importInvite: async (uri) => {
    if (!uri.startsWith("dv://") || uri.length < 20) fail("bad_invite", "bad invite");
    configured = true;
    emit("status", status());
    return { needsRelay: false };
  },
  servers: async () => (configured ? servers() : []),
  probeServers: async () => {
    const list = configured ? servers().map((s, i) => ({ ...s, latencyMs: 40 + i * 12 })) : [];
    return list;
  },
  settings: async () => structuredClone(settings),
  setSettings: async (s) => {
    settings = structuredClone(s);
  },
  unblock: async () => {},
  reportDiagnostics: async () => ({ issueUrl: "https://github.com/payam-foundation/dotvpn-client/issues/new", via: "browser" }),
  installHelper: async () => {
    await new Promise((r) => setTimeout(r, frozen ? 60_000 : 600));
    helper = true;
    emit("helper", true);
  },
  removeHelper: async () => {
    helper = false;
    emit("helper", false);
  },
  account: async () => structuredClone(account),
  relays: async () => structuredClone(relays),
  addRelay: async (idOrURI) => {
    const id = idOrURI.replace(/^dotvpn-relay:\/\//, "").split(/[/?#]/)[0]?.trim() ?? "";
    if (!id) fail("bad_invite", "invalid relay id");
    if (!relays.some((r) => r.id === id)) {
      relays = [...relays, { id, name: id, tier: "community", health: "offline", public: false, optedIn: true }];
    } else {
      relays = relays.map((r) => (r.id === id ? { ...r, optedIn: true } : r));
    }
  },
  removeRelay: async (id) => {
    relays = relays.map((r) => (r.id === id ? { ...r, optedIn: false } : r)).filter((r) => r.tier === "system" || r.public || r.optedIn);
  },
  ads: async (slot) => {
    if (slot === "home") {
      return [{ id: "ad1", issuer: "owner", format: "card", title: "Dot VPN Premium", desc: "Go ad-free.", link: "https://dotvpn.org" }];
    }
    return [];
  },
  logout: async () => {
    configured = false;
    state = "off";
    error = "";
    account = { name: "", quotaLimit: 0, quotaUsed: 0, quotaRemaining: 0, maxDevices: 0, disabled: false };
    emit("status", status());
  },
  prefs: async () => structuredClone(prefs),
  setPrefs: async (p) => {
    prefs = structuredClone(p);
    emit("prefs", prefs);
  },
  takeInvite: async (): Promise<InviteResult | null> => null,
  helperLogPath: async () => "/var/log/dotvpn.log",
  openHelperLog: async () => {},
  paste: async () => "dv://eyJ2IjoxLCJub2RlIjoiZXhhbXBsZS5jb20iLCJrZXkiOiJBYkNkRWZHaElqS2xNbk9wIn0",
  on: (name, cb) => {
    const set = listeners[name] as Set<typeof cb>;
    set.add(cb);
    return () => set.delete(cb);
  },
};

// Test hook: Playwright moves the helper between states without waiting.
(window as unknown as { __mock: unknown }).__mock = {
  setState(s: State, err = "") {
    state = s;
    error = err;
    emit("status", status());
  },
  setHelper(up: boolean) {
    helper = up;
    emit("helper", up);
  },
};
