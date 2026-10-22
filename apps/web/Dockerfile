# Production image for the ZC2X web dashboard (Next.js). A standalone app,
# not a pnpm/turbo monorepo -- unlike SmartCampusMaua/web's Dockerfile this
# needs no workspace-pruning stage, so the build context is this directory
# alone.
#
# Build from THIS directory:
#   docker build --build-arg NEXT_PUBLIC_NATS_URL=ws://localhost:4223 -t zc2x/web:local .
# (infra/docker/docker-compose.yaml does this automatically.)

FROM node:22-alpine AS base
RUN npm install -g pnpm@10

# --- Install deps ---

FROM base AS deps
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# --- Build ---

FROM base AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .

# NEXT_PUBLIC_* vars are inlined into the client bundle at build time, so
# they have to be build args, not runtime environment -- setting them only
# on the runner stage below would leave the browser bundle pointing at
# whatever value (or none) was present when `next build` ran.
ARG NEXT_PUBLIC_NATS_URL
ENV NEXT_PUBLIC_NATS_URL=$NEXT_PUBLIC_NATS_URL

RUN pnpm build

# --- Lean runtime image (Next.js standalone output) ---

FROM node:22-alpine AS runner
RUN addgroup --system --gid 1001 nodejs && adduser --system --uid 1001 nextjs
WORKDIR /app

COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public ./public

USER nextjs
EXPOSE 3000
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

CMD ["node", "server.js"]
