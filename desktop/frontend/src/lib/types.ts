// Mirrors internal/ipc/types.go and the desktop bindings. The frontend never
// sees tokens or node addresses: the helper redacts them.

export type State = "off" | "connecting" | "switching" | "connected" | "failed";

export interface Status {
  state: State;
  running: boolean;
  error?: string;
  carrier?: string;
  server?: string;
  country?: string;
  viaRelay?: string;
  viaRelayTier?: string;
  viaRelayId?: string;
  latencyMs?: number;
  since?: string;
  exitIp?: string;
  publicIp?: string;
  upBytes: number;
  downBytes: number;
  fullTunnel: boolean;
  killSwitch: boolean;
  configured: boolean;
  at: string;
}

export interface Server {
  id: string;
  name: string;
  country?: string;
  carrier: string;
  active: boolean;
  latencyMs?: number;
  error?: string;
  cooldownMs?: number;
}

export interface Account {
  name: string;
  package?: string;
  quotaLimit: number;
  quotaUsed: number;
  quotaRemaining: number;
  quotaReset?: string;
  expires?: number;
  maxDevices: number;
  devicesUsed?: number;
  mbps?: number;
  disabled: boolean;
}

export interface Ad {
  id: string;
  issuer: string;
  relayId?: string;
  format: string;
  imageUrl?: string;
  iconUrl?: string;
  title?: string;
  desc?: string;
  link?: string;
}

export interface Relay {
  id: string;
  name: string;
  country?: string;
  tier: string;
  health: string;
  invite?: string;
  public: boolean;
  optedIn: boolean;
}

export interface ImportResult {
  needsRelay?: boolean;
}

export interface Settings {
  fullTunnel: boolean;
  killSwitch: boolean;
  dns: string;
  dnsPreset?: string;
  doh: boolean;
  /** Host resolver Dot VPN sees when preset is system (read-only). */
  systemDns?: string;
  pin?: string;
  useRelays: boolean;
  carriers?: string[];
  directDomains: string[];
  directCidrs: string[];
  crashReporting?: boolean;
}

export interface ReportView {
  id: string;
  address: string;
  preview?: string;
  armor?: string;
}

export type Lang = "en" | "fa";
export type Theme = "system" | "light" | "dark";

export interface Prefs {
  language: Lang | "";
  theme: Theme;
  startAtLogin: boolean;
  startMinimized: boolean;
}

export interface Info {
  os: "darwin" | "linux" | "windows";
  version: string;
  trayHidden: boolean;
}

export interface InviteResult {
  ok: boolean;
  code?: string;
  message?: string;
}

export interface AppError {
  code: string;
  message: string;
}

export const emptyStatus: Status = {
  state: "off",
  running: false,
  upBytes: 0,
  downBytes: 0,
  fullTunnel: false,
  killSwitch: false,
  configured: false,
  at: "",
};

export const emptySettings: Settings = {
  fullTunnel: true,
  killSwitch: false,
  dns: "",
  dnsPreset: "cloudflare",
  doh: false,
  useRelays: false,
  carriers: [],
  directDomains: [],
  directCidrs: [],
  crashReporting: false,
};

export const emptyAccount: Account = {
  name: "",
  quotaLimit: 0,
  quotaUsed: 0,
  quotaRemaining: 0,
  maxDevices: 0,
  disabled: false,
};
