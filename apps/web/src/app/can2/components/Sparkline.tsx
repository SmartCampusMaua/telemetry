import type { Point } from "../telemetry-store";

// A hand-rolled inline SVG instead of mounting a recharts instance per
// signal: a dashboard grouped by CAN message can easily show 100+ signal
// cards at once (this project's DBC alone defines 284 signals across 52
// messages), and a full chart-library instance per card is real overhead
// multiplied that many times over for what's ultimately a few dozen points
// and one polyline.
export function Sparkline({ points }: { points: Point[] }) {
  const width = 96;
  const height = 28;

  if (points.length < 2) {
    return <svg width={width} height={height} aria-hidden />;
  }

  const values = points.map((p) => p.v);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1; // avoid divide-by-zero when the signal is flat

  const coords = points.map((p, i) => {
    const x = (i / (points.length - 1)) * width;
    const y = height - ((p.v - min) / span) * height;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });

  return (
    <svg width={width} height={height} className="text-primary" aria-hidden>
      <polyline
        points={coords.join(" ")}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
