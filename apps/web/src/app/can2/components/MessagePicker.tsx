"use client";

import { useState } from "react";
import { useCatalog } from "../hooks";
import { signalKey } from "../use-selection";

// The "quick menu" of which CAN messages/signals to render, MoTeC i2-style:
// i2 doesn't dump every channel onto the screen either, you build a
// worksheet from a channel picker. Two levels: a message-level checkbox
// (select every signal in that CAN frame at once) that expands into
// individual per-signal checkboxes for cherry-picking specific sensor_types
// -- checking either adds a fully pre-rendered row immediately (see
// MessageGroupCard), never one that fills in as values happen to arrive.
export function MessagePicker({
  selectedSignals,
  onToggleSignal,
  onToggleMessage,
}: {
  selectedSignals: Set<string>;
  onToggleSignal: (canMessageName: string, sensorType: string, on: boolean) => void;
  onToggleMessage: (canMessageName: string, sensorTypes: string[], on: boolean) => void;
}) {
  const catalog = useCatalog();
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const messages = Array.from(catalog.values()).sort((a, b) => a.canMessageId - b.canMessageId);

  if (messages.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        No CAN messages seen yet — this fills in as telemetry arrives, and
        remembers what it&apos;s seen next time you load this page.
      </p>
    );
  }

  function toggleExpanded(canMessageName: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(canMessageName)) {
        next.delete(canMessageName);
      } else {
        next.add(canMessageName);
      }
      return next;
    });
  }

  return (
    <ul className="flex max-h-[28rem] flex-col gap-0.5 overflow-y-auto">
      {messages.map((m) => {
        const selectedCount = m.sensorTypes.filter((s) =>
          selectedSignals.has(signalKey(m.canMessageName, s))
        ).length;
        const allSelected = selectedCount === m.sensorTypes.length;
        const someSelected = selectedCount > 0 && !allSelected;
        const isExpanded = expanded.has(m.canMessageName);

        return (
          <li key={m.canMessageName}>
            <div className="flex items-center gap-1 rounded-md px-1 py-1 hover:bg-foreground/5">
              <button
                type="button"
                onClick={() => toggleExpanded(m.canMessageName)}
                className="w-4 shrink-0 text-xs text-muted-foreground"
                aria-label={isExpanded ? "Collapse signals" : "Expand signals"}
                aria-expanded={isExpanded}
              >
                {isExpanded ? "▾" : "▸"}
              </button>
              <label className="flex flex-1 cursor-pointer items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={allSelected}
                  ref={(el) => {
                    if (el) el.indeterminate = someSelected;
                  }}
                  onChange={(e) => onToggleMessage(m.canMessageName, m.sensorTypes, e.target.checked)}
                />
                <span className="flex-1 truncate">
                  <span className="uppercase">{m.canMessageName}</span>{" "}
                  <span className="text-muted-foreground">({m.canMessageId})</span>
                </span>
                <span className="shrink-0 text-xs text-muted-foreground">
                  {selectedCount}/{m.sensorTypes.length}
                </span>
              </label>
            </div>
            {isExpanded && (
              <ul className="ml-5 flex flex-col gap-0.5 border-l border-border/50 pl-2">
                {m.sensorTypes.map((sensorType) => (
                  <li key={sensorType}>
                    <label className="flex cursor-pointer items-center gap-2 py-0.5 text-xs">
                      <input
                        type="checkbox"
                        checked={selectedSignals.has(signalKey(m.canMessageName, sensorType))}
                        onChange={(e) => onToggleSignal(m.canMessageName, sensorType, e.target.checked)}
                      />
                      <span className="truncate">{sensorType}</span>
                    </label>
                  </li>
                ))}
              </ul>
            )}
          </li>
        );
      })}
    </ul>
  );
}
