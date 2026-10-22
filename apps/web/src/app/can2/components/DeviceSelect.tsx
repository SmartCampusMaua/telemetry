"use client";

import { useDeviceList } from "../hooks";

// A native <select>, not a checkbox list: a <select> always has exactly one
// current value, so there's no "everything unchecked" state to define
// behavior for. That ambiguity was a real bug in the checkbox version --
// unchecking the only device fell back to showing it anyway, which looked
// like the checkbox had no effect at all.
//
// Options are keyed by (device_id, origin), not device_id alone: RSU relays
// OBU's packet bytes unmodified (RFC-0001), so RSU-relayed telemetry's
// device_id is always OBU's own, never RSU's -- origin ("obu" direct WiFi
// vs "rsu" XBee relay) is the only thing that distinguishes the two paths,
// so the same physical device can legitimately show up here as two entries,
// one per path currently seen.
export function DeviceSelect({
  activeDeviceKey,
  onChange,
}: {
  activeDeviceKey: string | null;
  onChange: (devKey: string) => void;
}) {
  const devices = useDeviceList();

  if (devices.length === 0) {
    return <p className="text-xs text-muted-foreground">No devices seen yet.</p>;
  }

  return (
    <select
      value={activeDeviceKey ?? ""}
      onChange={(e) => onChange(e.target.value)}
      className="w-full rounded-md border border-input bg-card px-2 py-1.5 text-sm"
    >
      {devices.map((d) => (
        <option key={d.key} value={d.key}>
          {d.assetId ?? d.deviceId} — {d.origin} · {d.deviceId}
        </option>
      ))}
    </select>
  );
}
