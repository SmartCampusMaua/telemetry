import type { ConnectionStatus } from "../telemetry-store";

const LABEL: Record<ConnectionStatus, string> = {
  connecting: "Connecting…",
  connected: "Live",
  error: "Connection error",
};

const DOT: Record<ConnectionStatus, string> = {
  connecting: "bg-amber-500 animate-pulse",
  connected: "bg-emerald-500",
  error: "bg-red-500",
};

export function ConnectionBadge({ status }: { status: ConnectionStatus }) {
  return (
    <div className="flex items-center gap-2 text-sm text-muted-foreground">
      <span className={`h-2 w-2 rounded-full ${DOT[status]}`} aria-hidden />
      {LABEL[status]}
    </div>
  );
}
