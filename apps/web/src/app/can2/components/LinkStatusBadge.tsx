import type { LinkStatus } from "../hooks";

const LABEL: Record<LinkStatus, string> = {
  online: "Online",
  offline: "Offline",
  unknown: "Not seen yet",
};

const DOT: Record<LinkStatus, string> = {
  online: "bg-emerald-500",
  offline: "bg-red-500",
  unknown: "bg-muted-foreground/40",
};

// One row per relay path (OBU's direct WiFi link, RSU's XBee relay), not
// per device: liveness here is "is this link delivering anything at all
// right now" -- see hooks.ts's useLinkStatus -- independent of which
// specific device(s) are currently selected in the worksheet above.
export function LinkStatusBadge({ label, status }: { label: string; status: LinkStatus }) {
  return (
    <div className="flex items-center gap-2 text-sm text-muted-foreground">
      <span className={`h-2 w-2 rounded-full ${DOT[status]}`} aria-hidden />
      {label}: {LABEL[status]}
    </div>
  );
}
