"use client";

import { useEffect, useState } from "react";

// Calling Date.now() directly in a render body is an impure read (flagged
// by the react-hooks/purity lint rule) -- it makes the component's output
// depend on something outside props/state, which breaks React's
// idempotency assumptions for the same render. This hook moves that impure
// read into a useEffect (where side effects belong) and exposes the result
// as ordinary state, so components can derive "is this stale" purely from
// props + this state on every render.
export function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}
