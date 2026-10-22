// Mirrors services/input/nats/internal/signals.go's TelemetryRecord field
// for field (see that file's doc for the full JSON shape/example). This is
// the *decoded* per-signal stream (sensor_type already resolved from the
// DBC), published to zc2x.js.telemetry.> -- unlike the rest of this app's
// dashboards (src/app/{motor,eletrica,dynamics,gps}), which re-decode raw
// CAN bytes off zc2x.can.rsu themselves (see src/lib/can.ts). can2
// deliberately consumes the already-decoded stream instead: one JSON
// record per signal, keyed by sensor_type, with no per-message-type
// decode logic to keep in sync with the DBC by hand.
export type ValueKind = "float" | "int" | "bool" | "";

export type TelemetryRecord = {
  device_id: string;
  asset_id?: string;
  origin: string;
  can_message_id: number;
  can_message_name: string;
  sensor_type: string;
  value: number;
  value_kind: ValueKind;
  timestamp_ms: number;
};

export function isTelemetryRecord(v: unknown): v is TelemetryRecord {
  if (typeof v !== "object" || v === null) return false;
  const r = v as Record<string, unknown>;
  return (
    typeof r.sensor_type === "string" &&
    typeof r.can_message_name === "string" &&
    typeof r.value === "number" &&
    typeof r.timestamp_ms === "number"
  );
}
