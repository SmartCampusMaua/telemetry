"use client";

import type { SignalState } from "../telemetry-store";
import { useNow } from "../use-now";
import { Sparkline } from "./Sparkline";

// "engine_rpm" -> "Engine Rpm". Not trying to reverse candb's toSnakeCase
// acronym-handling exactly (that lives server-side, see
// services/input/nats/internal/signals.go) -- just readable, not
// byte-perfect capitalization of e.g. "RPM".
function titleCase(sensorType: string): string {
  return sensorType
    .split("_")
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}

function formatValue(signal: SignalState): string {
  const { latest, kind } = signal;
  switch (kind) {
    case "bool":
      return latest.v !== 0 ? "On" : "Off";
    case "int":
      return latest.v.toFixed(0);
    default:
      return latest.v.toFixed(3);
  }
}

// signal is undefined when this device hasn't sent this particular sensor
// yet this session -- the row still renders (title + a "--" placeholder),
// which is the whole point of driving rows from the catalog's known signal
// list (see MessageGroupCard) instead of only ever growing rows in as
// values arrive.
export function SignalRow({
  sensorType,
  signal,
}: {
  sensorType: string;
  signal: SignalState | undefined;
}) {
  const now = useNow(1000);
  const stale = signal ? now - signal.latest.t > 5000 : false;
  return (
    <div className="flex items-center justify-between gap-3 py-1.5">
      <div className="min-w-0">
        <div className="truncate text-sm">{titleCase(sensorType)}</div>
        <div
          className={`font-mono text-lg tabular-nums ${!signal || stale ? "text-muted-foreground" : ""}`}
        >
          {signal ? formatValue(signal) : "--"}
        </div>
      </div>
      {signal && signal.kind !== "bool" && <Sparkline points={signal.history} />}
    </div>
  );
}
