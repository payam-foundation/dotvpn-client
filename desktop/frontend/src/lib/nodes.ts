import type { Server } from "./types";

/** One picker row: node name + country, best latency among carriers. */
export interface NodeGroup {
  name: string;
  country?: string;
  /** Preferred endpoint id under this name (pin target). */
  id: string;
  latencyMs?: number;
  active: boolean;
  ids: string[];
}

/** Group ladder rungs by Endpoint.Name for the Home node picker. */
export function groupNodes(servers: Server[]): NodeGroup[] {
  const map = new Map<string, NodeGroup>();
  for (const s of servers) {
    const key = s.name || s.id;
    let g = map.get(key);
    if (!g) {
      g = { name: key, country: s.country, id: s.id, latencyMs: s.latencyMs, active: s.active, ids: [s.id] };
      map.set(key, g);
      continue;
    }
    g.ids.push(s.id);
    if (s.country && !g.country) g.country = s.country;
    if (s.active) {
      g.active = true;
      g.id = s.id;
      g.latencyMs = s.latencyMs;
    } else if (!g.active && s.latencyMs && (!g.latencyMs || s.latencyMs < g.latencyMs)) {
      g.latencyMs = s.latencyMs;
      g.id = s.id;
    }
  }
  return [...map.values()];
}
