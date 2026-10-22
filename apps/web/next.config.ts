import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Emits .next/standalone -- a self-contained server.js plus only the
  // node_modules it actually traces as used, instead of the full
  // node_modules tree. Dockerfile's runner stage copies exactly that, so
  // the image doesn't carry pnpm/the whole dependency tree at runtime.
  output: "standalone",
};

export default nextConfig;
