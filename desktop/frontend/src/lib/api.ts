import { Call, Clipboard, Events } from "@wailsio/runtime";
import type { Account, Ad, AppError, ImportResult, Info, InviteResult, Prefs, Relay, ReportResult, Server, Settings, Status } from "./types";

export type EventMap = {
  status: Status;
  helper: boolean;
  invite: InviteResult;
  prefs: Prefs;
};

/** Everything the window can ask of the desktop app. Mirrors desktop/bindings.go. */
export interface Api {
  info(): Promise<Info>;
  status(): Promise<Status>;
  connect(): Promise<void>;
  disconnect(): Promise<void>;
  importInvite(uri: string): Promise<ImportResult>;
  servers(): Promise<Server[]>;
  probeServers(): Promise<Server[]>;
  settings(): Promise<Settings>;
  setSettings(s: Settings): Promise<void>;
  unblock(): Promise<void>;
  reportDiagnostics(): Promise<ReportResult>;
  installHelper(): Promise<void>;
  removeHelper(): Promise<void>;
  account(): Promise<Account>;
  relays(): Promise<Relay[]>;
  addRelay(idOrURI: string): Promise<void>;
  removeRelay(id: string): Promise<void>;
  ads(slot: string, relayId?: string): Promise<Ad[]>;
  logout(): Promise<void>;
  prefs(): Promise<Prefs>;
  setPrefs(p: Prefs): Promise<void>;
  takeInvite(): Promise<InviteResult | null>;
  paste(): Promise<string>;
  helperLogPath(): Promise<string>;
  openHelperLog(): Promise<void>;
  on<K extends keyof EventMap>(name: K, cb: (data: EventMap[K]) => void): () => void;
}

const svc = "main.AppService.";
const call = <T>(method: string, ...args: unknown[]): Promise<T> => Call.ByName(svc + method, ...args);

const wails: Api = {
  info: () => call("Info"),
  status: () => call("Status"),
  connect: () => call("Connect"),
  disconnect: () => call("Disconnect"),
  importInvite: (uri) => call<ImportResult>("ImportInvite", uri),
  servers: () => call("Servers"),
  probeServers: () => call("ProbeServers"),
  settings: () => call("Settings"),
  setSettings: (s) => call("SetSettings", s),
  unblock: () => call("Unblock"),
  reportDiagnostics: () => call("ReportDiagnostics"),
  installHelper: () => call("InstallHelper"),
  removeHelper: () => call("RemoveHelper"),
  account: () => call("Account"),
  relays: () => call("Relays"),
  addRelay: (id) => call("AddRelay", id),
  removeRelay: (id) => call("RemoveRelay", id),
  ads: (slot, relayId = "") => call("Ads", slot, relayId),
  logout: () => call("Logout"),
  prefs: () => call("Prefs"),
  setPrefs: (p) => call("SetPrefs", p),
  takeInvite: () => call("TakeInvite"),
  paste: () => Clipboard.Text(),
  helperLogPath: () => call("HelperLogPath"),
  openHelperLog: () => call("OpenHelperLog"),
  on: (name, cb) => Events.On(name, (ev) => cb(ev.data)),
};

/** The real bindings, or the in-memory mock when built with `--mode mock`. */
export const api: Api = import.meta.env.VITE_MOCK ? (await import("./mock")).mockApi : wails;

/** Wails puts the helper's JSON error in `cause`; anything else is internal. */
export function toAppError(e: unknown): AppError {
  const cause = (e as { cause?: unknown } | null)?.cause;
  if (cause && typeof cause === "object" && "code" in cause) {
    const c = cause as Partial<AppError>;
    return { code: String(c.code), message: String(c.message ?? "") };
  }
  return { code: "internal", message: e instanceof Error ? e.message : String(e) };
}
