"use client";

import { useActiveDeviceKey, useConnectionStatus, useLinkStatus } from "../hooks";
import { useSelectedDevice, useSelectedSignals } from "../use-selection";
import { ConnectionBadge } from "./ConnectionBadge";
import { DeviceSelect } from "./DeviceSelect";
import { LinkStatusBadge } from "./LinkStatusBadge";
import { MessagePicker } from "./MessagePicker";

// Rendered from layout.tsx, not page.tsx -- see that file's doc for why:
// Next.js layouts persist across the content they wrap, which is exactly
// "stays fixed while the page scrolls" at the framework level, not just a
// CSS trick applied to a component that still lives inside the scrolling
// page content.
export function Sidebar() {
  const status = useConnectionStatus();
  const obuStatus = useLinkStatus("obu");
  const rsuStatus = useLinkStatus("rsu");
  const activeDeviceKey = useActiveDeviceKey();
  const [, setSelectedDeviceKey] = useSelectedDevice();
  const { selected: selectedSignals, toggleSignal, toggleMessage } = useSelectedSignals();

  return (
    <div className="flex h-full flex-col gap-6 overflow-y-auto p-6">
      <div className="flex flex-col gap-1.5">
        <ConnectionBadge status={status} />
        <LinkStatusBadge label="OBU" status={obuStatus} />
        <LinkStatusBadge label="RSU" status={rsuStatus} />
      </div>
      <div>
        <h2 className="mb-2 text-sm font-semibold text-muted-foreground">Device</h2>
        <DeviceSelect activeDeviceKey={activeDeviceKey} onChange={setSelectedDeviceKey} />
      </div>
      <div className="min-h-0 flex-1">
        <h2 className="mb-2 text-sm font-semibold text-muted-foreground">
          CAN Messages
        </h2>
        <MessagePicker
          selectedSignals={selectedSignals}
          onToggleSignal={toggleSignal}
          onToggleMessage={toggleMessage}
        />
      </div>
    </div>
  );
}
