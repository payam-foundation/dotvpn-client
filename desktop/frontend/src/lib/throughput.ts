export interface Sample {
  down: number;
  up: number;
}

export interface Counters {
  up: number;
  down: number;
  at: number;
}

export const MAX_SAMPLES = 60;

/**
 * Turns two counter readings into bytes per second. Counters that went
 * backwards mean a new session, so that reading is a restart, not a sample.
 */
export function rate(prev: Counters | null, cur: Counters): Sample | null {
  if (!prev) return null;
  const dt = (cur.at - prev.at) / 1000;
  if (dt <= 0 || cur.up < prev.up || cur.down < prev.down) return null;
  return { up: (cur.up - prev.up) / dt, down: (cur.down - prev.down) / dt };
}

export function push(samples: Sample[], s: Sample): Sample[] {
  const next = [...samples, s];
  return next.length > MAX_SAMPLES ? next.slice(next.length - MAX_SAMPLES) : next;
}
