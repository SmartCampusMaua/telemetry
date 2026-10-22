"use client";

import { useSyncExternalStore } from "react";

// UI preference, not telemetry data -- deliberately separate from
// telemetry-store.ts (which only ever holds data observed from NATS).
// Persisted so a reload doesn't lose the "worksheet" you built, the same
// way MoTeC i2 remembers your worksheet layout between sessions.
const SELECTED_SIGNALS_KEY = "zc2x:can2:selected-signals:v1";
// v3, not v2: v2 persisted a raw device_id. The value now persisted is a
// compound (device_id, origin) key (see telemetry-store.ts's deviceKey) --
// a v2 value would silently fail to match any device.key in the current
// device list and just fall back to the first device, which is harmless,
// but the version bump keeps this key's format self-documenting the way
// v1 -> v2 already established for this same storage slot.
const SELECTED_DEVICE_KEY = "zc2x:can2:selected-device:v3";

export function signalKey(canMessageName: string, sensorType: string): string {
  return `${canMessageName} ${sensorType}`;
}

function loadStringSet(storageKey: string): Set<string> {
  if (typeof window === "undefined") return new Set();
  try {
    const raw = window.localStorage.getItem(storageKey);
    return raw ? new Set(JSON.parse(raw) as string[]) : new Set();
  } catch {
    return new Set();
  }
}

function saveStringSet(storageKey: string, value: Set<string>) {
  try {
    window.localStorage.setItem(storageKey, JSON.stringify(Array.from(value)));
  } catch {
    // Storage full/unavailable/private-mode -- best effort only.
  }
}

const EMPTY_SIGNALS: Set<string> = new Set();

// A module-level singleton, not per-component useState: the sidebar (device
// select + message picker, rendered from layout.tsx so it stays fixed while
// the page scrolls -- see that file) and the cards area (rendered from
// page.tsx) are siblings now, not parent/child, so they can't share state
// via props. Same useSyncExternalStore pattern as telemetry-store.ts, for
// the same reason: every subscriber needs to see the same live value.
class SelectionStore {
  private signals: Set<string> = loadStringSet(SELECTED_SIGNALS_KEY);
  private signalsListeners = new Set<() => void>();

  private device: string | null =
    typeof window === "undefined" ? null : window.localStorage.getItem(SELECTED_DEVICE_KEY);
  private deviceListeners = new Set<() => void>();

  subscribeSignals = (listener: () => void): (() => void) => {
    this.signalsListeners.add(listener);
    return () => this.signalsListeners.delete(listener);
  };
  getSignalsSnapshot = (): Set<string> => this.signals;
  getServerSignalsSnapshot = (): Set<string> => EMPTY_SIGNALS;

  subscribeDevice = (listener: () => void): (() => void) => {
    this.deviceListeners.add(listener);
    return () => this.deviceListeners.delete(listener);
  };
  getDeviceSnapshot = (): string | null => this.device;
  getServerDeviceSnapshot = (): string | null => null;

  toggleSignal = (canMessageName: string, sensorType: string, on: boolean): void => {
    const next = new Set(this.signals);
    const key = signalKey(canMessageName, sensorType);
    if (on) {
      next.add(key);
    } else {
      next.delete(key);
    }
    this.signals = next;
    saveStringSet(SELECTED_SIGNALS_KEY, next);
    this.signalsListeners.forEach((l) => l());
  };

  toggleMessage = (canMessageName: string, sensorTypes: string[], on: boolean): void => {
    const next = new Set(this.signals);
    for (const sensorType of sensorTypes) {
      const key = signalKey(canMessageName, sensorType);
      if (on) {
        next.add(key);
      } else {
        next.delete(key);
      }
    }
    this.signals = next;
    saveStringSet(SELECTED_SIGNALS_KEY, next);
    this.signalsListeners.forEach((l) => l());
  };

  setDevice = (devKey: string): void => {
    this.device = devKey;
    try {
      window.localStorage.setItem(SELECTED_DEVICE_KEY, devKey);
    } catch {
      // best effort only
    }
    this.deviceListeners.forEach((l) => l());
  };
}

const selectionStore = new SelectionStore();

export function useSelectedSignals(): {
  selected: Set<string>;
  toggleSignal: (canMessageName: string, sensorType: string, on: boolean) => void;
  toggleMessage: (canMessageName: string, sensorTypes: string[], on: boolean) => void;
} {
  const selected = useSyncExternalStore(
    selectionStore.subscribeSignals,
    selectionStore.getSignalsSnapshot,
    selectionStore.getServerSignalsSnapshot
  );
  return { selected, toggleSignal: selectionStore.toggleSignal, toggleMessage: selectionStore.toggleMessage };
}

// A <select> always has exactly one current value, so there's no
// "everything unchecked" state to define behavior for -- see DeviceSelect's
// doc for why that ambiguity was a real bug in an earlier checkbox version.
// The persisted/returned value is a compound (device_id, origin) key (see
// telemetry-store.ts's deviceKey), not a raw device_id.
export function useSelectedDevice(): [string | null, (devKey: string) => void] {
  const selected = useSyncExternalStore(
    selectionStore.subscribeDevice,
    selectionStore.getDeviceSnapshot,
    selectionStore.getServerDeviceSnapshot
  );
  return [selected, selectionStore.setDevice];
}
