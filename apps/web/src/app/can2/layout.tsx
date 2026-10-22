import type { Metadata } from "next";
import { Sidebar } from "./components/Sidebar";

// Nested App Router layout -- no <html>/<body> here, the root layout
// (src/app/layout.tsx, untouched) already provides those. This just adds
// the page shell for this route segment.
export const metadata: Metadata = {
  title: "ZC2X | CAN2 Live Telemetry",
  description:
    "Live decoded CAN2 telemetry (zc2x.js.telemetry.>), grouped by device and CAN message.",
};

export default function Can2Layout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    // h-dvh (not h-screen): stays correct on mobile browsers whose chrome
    // shows/hides on scroll, which changes the viewport height h-screen
    // freezes at load time. overflow-hidden here + overflow-y-auto on the
    // sidebar/main below is what makes them scroll independently instead of
    // the whole page scrolling as one unit -- without it, the flex row's
    // height would just grow with its tallest child (the cards grid) and
    // drag the sidebar down the page with it.
    <div className="flex h-dvh flex-col overflow-hidden">
      <header className="shrink-0 border-b border-border/50 p-6 pb-4">
        <h1 className="text-xl font-semibold">CAN2 Live Telemetry</h1>
        <p className="text-sm text-muted-foreground">
          Decoded signals streamed live from NATS, grouped by device and CAN
          message.
        </p>
      </header>
      {/* min-h-0 overrides flex items' default min-height:auto, which would
          otherwise let this row grow taller than the viewport instead of
          being clipped to exactly the space left under the header. */}
      <div className="flex min-h-0 flex-1">
        <aside className="w-64 shrink-0 border-r border-border/50">
          <Sidebar />
        </aside>
        <main className="min-w-0 flex-1 overflow-y-auto p-6">{children}</main>
      </div>
    </div>
  );
}
