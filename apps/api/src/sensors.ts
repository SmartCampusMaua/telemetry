// Loads the sensor/device/asset registry from Postgres (src/db/schema.ts) — the same tables
// src/routes/registry.ts serves over HTTP — instead of the sibling parse-mqtt-to-influxdb3
// repo's sensors.json/devices.json/assets.json, which this used to read directly via hardcoded
// relative paths (a local-dev-only arrangement that only worked when both repos were checked out
// side by side, and never worked inside a Docker image). Postgres is the real source of truth
// now; see scripts/import-registry.ts for the one-time bootstrap that originally populated it
// from those same JSON files.
//
// Unlike config.ts's infra.json, a bad/missing load here does NOT crash the process: this
// registry is a convenience for the sensor-picker UI and the /:org/sensors endpoint, not
// something the core timeseries routes depend on to function. A failed load should degrade that
// one feature, not take the whole API down.

import { eq, or } from "drizzle-orm";
import { db } from "./db/client";
import { assets, devices, sensors } from "./db/schema";

export type SensorInfo = {
  sensor_id: string;
  sensor_type: string;
  sensor_name?: string;
  device_id: string;
  device_model?: string;
  dev_eui?: string;
  mac_address?: string;
  asset_id?: string;
  asset_name?: string;
  asset_slug?: string;
};

const REFRESH_SEC = Number(process.env.SENSORS_CONFIG_REFRESH_SEC) || 30;

// Effective asset for a sensor is sensor.asset_id if set, else its device's own asset_id (see
// schema.ts's comment on sensors.asset_id) — resolved here with a single left join on each
// rather than two round trips + an in-memory join, now that both tables live in the same
// database.
async function loadRegistry(): Promise<SensorInfo[]> {
  const rows = await db
    .select({
      sensor_id: sensors.id,
      sensor_type: sensors.sensor_type,
      sensor_name: sensors.sensor_name,
      device_id: sensors.device_id,
      device_model: devices.device_model,
      dev_eui: devices.dev_eui,
      mac_address: devices.mac_address,
      asset_id: sensors.asset_id,
      device_asset_id: devices.asset_id,
    })
    .from(sensors)
    .innerJoin(devices, eq(sensors.device_id, devices.id));

  const effectiveAssetIds = [...new Set(rows.map((r) => r.asset_id ?? r.device_asset_id).filter((id): id is string => !!id))];
  const assetRows = effectiveAssetIds.length
    ? await db
        .select({ id: assets.id, asset_name: assets.asset_name, asset_slug: assets.asset_slug })
        .from(assets)
        .where(or(...effectiveAssetIds.map((id) => eq(assets.id, id))))
    : [];
  const assetById = new Map(assetRows.map((a) => [a.id, a]));

  return rows.map((r) => {
    const effectiveAssetId = r.asset_id ?? r.device_asset_id ?? undefined;
    const asset = effectiveAssetId ? assetById.get(effectiveAssetId) : undefined;
    return {
      sensor_id: r.sensor_id,
      sensor_type: r.sensor_type,
      sensor_name: r.sensor_name ?? undefined,
      device_id: r.device_id,
      device_model: r.device_model,
      dev_eui: r.dev_eui ?? undefined,
      mac_address: r.mac_address ?? undefined,
      asset_id: effectiveAssetId,
      asset_name: asset?.asset_name,
      asset_slug: asset?.asset_slug,
    };
  });
}

// Four lookups built in one pass over `list`, all keyed for O(1) access instead of an O(registry
// size) scan per row/request — sensorIdIndex gets called once per reading/tuple in a series
// response, and deviceIdIndex/assetIdIndex/assetSlugIndex back the /sensors endpoint's own
// ?deviceId=/?assetId=/?assetSlug= filters (see below), so all need to stay cheap even as the
// registry grows toward thousands of sensors. Rebuilt alongside `registry` on every load/reload,
// never mutated in place (same reasoning as config.ts's `orgs`: reassignment is atomic, so a
// reader never sees a half-rebuilt index).
let sensorIdIndex = new Map<string, string[]>(); // "${device_id}::${sensor_type}" -> sensor_id[]
let deviceIdIndex = new Map<string, SensorInfo[]>(); // device_id -> every sensor on that device
let assetIdIndex = new Map<string, SensorInfo[]>(); // asset_id -> every sensor on that asset's devices
let assetSlugIndex = new Map<string, SensorInfo[]>(); // asset_slug -> same, keyed by the human-typeable slug instead

function buildIndexes(list: SensorInfo[]) {
  const sensorIds = new Map<string, string[]>();
  const deviceIds = new Map<string, SensorInfo[]>();
  const assetIds = new Map<string, SensorInfo[]>();
  const assetSlugs = new Map<string, SensorInfo[]>();
  for (const s of list) {
    const sensorKey = `${s.device_id}::${s.sensor_type}`;
    const sensorMatches = sensorIds.get(sensorKey);
    if (sensorMatches) sensorMatches.push(s.sensor_id);
    else sensorIds.set(sensorKey, [s.sensor_id]);

    const deviceMatches = deviceIds.get(s.device_id);
    if (deviceMatches) deviceMatches.push(s);
    else deviceIds.set(s.device_id, [s]);

    if (s.asset_id) {
      const assetMatches = assetIds.get(s.asset_id);
      if (assetMatches) assetMatches.push(s);
      else assetIds.set(s.asset_id, [s]);
    }

    if (s.asset_slug) {
      const slugMatches = assetSlugs.get(s.asset_slug);
      if (slugMatches) slugMatches.push(s);
      else assetSlugs.set(s.asset_slug, [s]);
    }
  }
  return { sensorIds, deviceIds, assetIds, assetSlugs };
}

function applyIndexes(list: SensorInfo[]): void {
  const { sensorIds, deviceIds, assetIds, assetSlugs } = buildIndexes(list);
  sensorIdIndex = sensorIds;
  deviceIdIndex = deviceIds;
  assetIdIndex = assetIds;
  assetSlugIndex = assetSlugs;
}

// Resolves a row's sensor_id from the registry when InfluxDB's own sensor_id column doesn't have
// it yet (see the sensorId footnote in README.md) — device_id + sensor_type alone only
// unambiguously identifies a sensor for the common case of one sensor per type per device.
// Multi-probe devices (e.g. three soil_moisture_raw entries on one lnv3_sm3dl, disambiguated in
// the registry by device_index/sensor_name but *not* by anything sensor_data itself stores) have
// more than one candidate here, and there's no way to tell them apart from a plain InfluxDB row
// — returning any one of them would be a guess dressed up as data, so this returns undefined for
// that case rather than a plausible-looking wrong sensor_id.
export function resolveSensorId(deviceId: string, sensorType: string): string | undefined {
  const matches = sensorIdIndex.get(`${deviceId}::${sensorType}`);
  return matches?.length === 1 ? matches[0] : undefined;
}

// Backs the /sensors endpoint's ?deviceId=/?assetId=/?assetSlug= filters — an unknown id/slug is
// a normal empty result, not an error, same as any other tag filter elsewhere in this API.
export function sensorsByDeviceId(deviceId: string): SensorInfo[] {
  return deviceIdIndex.get(deviceId) ?? [];
}
export function sensorsByAssetId(assetId: string): SensorInfo[] {
  return assetIdIndex.get(assetId) ?? [];
}
// The main timeseries/export routes resolve ?assetSlug= into the equivalent asset_id(s) via this
// (see resolveAssetSlugFilter in routes/timeseries.ts) — InfluxDB itself only ever has asset_id as
// a real tag/column, never asset_slug, so the slug never reaches a SQL query directly.
export function sensorsByAssetSlug(assetSlug: string): SensorInfo[] {
  return assetSlugIndex.get(assetSlug) ?? [];
}

let registry: SensorInfo[] = [];
let loadError: string | undefined;

async function load(): Promise<void> {
  try {
    registry = await loadRegistry();
    applyIndexes(registry);
    loadError = undefined;
    console.log(`[sensors] loaded registry from Postgres: ${registry.length} sensors`);
  } catch (err) {
    loadError = err instanceof Error ? err.message : String(err);
    console.error(`[sensors] load failed, keeping previous registry: ${loadError}`);
  }
}

void load();
setInterval(load, REFRESH_SEC * 1000).unref();

export const sensorRegistry = {
  get list(): SensorInfo[] {
    return registry;
  },
  get error(): string | undefined {
    return loadError;
  },
};
