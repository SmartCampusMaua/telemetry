"use client";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useMessageState } from "../hooks";
import type { CatalogMessage } from "../telemetry-store";
import { SignalRow } from "./SignalRow";

// Subscribes to exactly this (devKey, canMessageName) topic -- see
// telemetry-store.ts's per-message subscription doc -- so a value update
// only re-renders this one card, not the whole dashboard. Renders one row
// per entry in sensorTypes (the signals actually checked in the picker for
// this message, a subset of message.sensorTypes), not per key present in
// the live snapshot: a freshly checked signal shows up immediately with a
// placeholder, rather than only appearing once a value happens to stream
// in.
export function MessageGroupCard({
  deviceKey,
  message,
  sensorTypes,
}: {
  deviceKey: string | null;
  message: CatalogMessage;
  sensorTypes: string[];
}) {
  const live = useMessageState(deviceKey, message.canMessageName);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="uppercase">{message.canMessageName}</CardTitle>
        <CardDescription>can_message_id {message.canMessageId}</CardDescription>
      </CardHeader>
      <CardContent className="divide-y divide-border/50">
        {sensorTypes.map((sensorType) => (
          <SignalRow key={sensorType} sensorType={sensorType} signal={live?.get(sensorType)} />
        ))}
      </CardContent>
    </Card>
  );
}
