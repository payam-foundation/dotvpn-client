/** Geo/ASN lookup for the IP shown on Home. Failures stay silent. */

export type IPMeta = {
  ip: string;
  country?: string;
  countryCode?: string;
  asn?: string;
  org?: string;
};

const cache = new Map<string, IPMeta>();
const inflight = new Map<string, Promise<IPMeta | null>>();

export async function lookupIP(ip: string): Promise<IPMeta | null> {
  const key = ip.trim();
  if (!key || key.includes(":") || !/^\d{1,3}(\.\d{1,3}){3}$/.test(key)) return null;
  const hit = cache.get(key);
  if (hit) return hit;
  const pending = inflight.get(key);
  if (pending) return pending;

  const p = (async () => {
    try {
      const res = await fetch(`https://ipwho.is/${encodeURIComponent(key)}`, {
        signal: AbortSignal.timeout(6_000),
      });
      if (!res.ok) return null;
      const j = (await res.json()) as {
        success?: boolean;
        ip?: string;
        country?: string;
        country_code?: string;
        connection?: { asn?: number; org?: string; isp?: string };
      };
      if (j.success === false) return null;
      const asnNum = j.connection?.asn;
      const meta: IPMeta = {
        ip: j.ip || key,
        country: j.country || undefined,
        countryCode: j.country_code || undefined,
        asn: asnNum ? `AS${asnNum}` : undefined,
        org: j.connection?.org || j.connection?.isp || undefined,
      };
      cache.set(key, meta);
      return meta;
    } catch {
      return null;
    } finally {
      inflight.delete(key);
    }
  })();
  inflight.set(key, p);
  return p;
}
