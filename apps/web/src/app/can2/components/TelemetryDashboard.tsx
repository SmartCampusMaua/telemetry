"use client";

import { useMemo } from "react";
import { useActiveDeviceKey, useCatalog } from "../hooks";
import type { CatalogMessage } from "../telemetry-store";
import { signalKey, useSelectedSignals } from "../use-selection";
import { MessageGroupCard } from "./MessageGroupCard";

type Card = { key: string; message: CatalogMessage; sensorTypes: string[] };

// Rendered from page.tsx, inside layout.tsx's scrolling <main> -- the
// sidebar (device select + message picker) lives in layout.tsx instead
// (see Sidebar.tsx), so it stays fixed while this content scrolls.
export function TelemetryDashboard() {
  const activeDeviceKey = useActiveDeviceKey();
  const catalog = useCatalog();
  const { selected: selectedSignals } = useSelectedSignals();

  const cards = useMemo<Card[]>(() => {
    const result: Card[] = [];
    for (const message of catalog.values()) {
      const sensorTypes = message.sensorTypes.filter((s) =>
        selectedSignals.has(signalKey(message.canMessageName, s))
      );
      if (sensorTypes.length === 0) continue;
      result.push({ key: message.canMessageName, message, sensorTypes });
    }
    return result.sort((a, b) => a.message.canMessageId - b.message.canMessageId);
  }, [catalog, selectedSignals]);

  if (cards.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        Pick one or more signals from the menu to add them to this worksheet.
      </p>
    );
  }

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
      {cards.map((c) => (
        <MessageGroupCard
          key={c.key}
          deviceKey={activeDeviceKey}
          message={c.message}
          sensorTypes={c.sensorTypes}
        />
      ))}
    </div>
  );
}
