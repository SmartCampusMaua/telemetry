# syntax=docker/dockerfile:1
#
# Multi-stage build — build context is this directory (apps/api), invoked as
# `docker build -f Dockerfile -t telemetry-apps-api .` from here, so every
# COPY below is relative to apps/api, not the repo root.
#
# Stages:
#   base      — shared starting point, nothing built yet.
#   install   — dependencies only, split into a "dev" tree (full, for typechecking) and a "prod"
#               tree (--production, for the final image) so Docker's layer cache only invalidates
#               on package.json/bun.lock changes, not on every source edit.
#   typecheck — runs `tsc --noEmit` as a build-time gate. Its output is never copied into the
#               final image — a type error fails the docker build (and so CI) instead of shipping.
#   release   — the actual runtime image: prod-only node_modules, source, no devDependencies,
#               no build tooling, non-root user.

FROM oven/bun:1-alpine AS base
WORKDIR /usr/src/app

FROM base AS install

RUN mkdir -p /temp/dev
COPY package.json bun.lock /temp/dev/
RUN cd /temp/dev && bun install --frozen-lockfile

RUN mkdir -p /temp/prod
COPY package.json bun.lock /temp/prod/
RUN cd /temp/prod && bun install --frozen-lockfile --production

FROM base AS typecheck
COPY --from=install /temp/dev/node_modules node_modules
COPY package.json bun.lock tsconfig.json ./
COPY src src
RUN bun run typecheck

FROM base AS release
ENV NODE_ENV=production
COPY --from=install /temp/prod/node_modules node_modules
COPY --from=typecheck /usr/src/app/src src
COPY package.json .

# infra.json is deliberately NOT copied in (see .dockerignore and the token-security warning
# in README.md) — same pattern as INFRA_CONFIG_FILE in the mqtt/chirpstackv4 services' own
# Dockerfiles: a fixed, well-known absolute path baked into the image, with the actual file
# supplied at runtime (a k8s Secret volume — see README.md's "Docker / Kubernetes" section for
# why Secret, not ConfigMap, and the subPath gotcha that silently breaks hot-reload). src/config.ts
# re-reads this path every INFRA_CONFIG_REFRESH_SEC and fails fast at startup if nothing's
# there, same as running locally with a missing infra.json.
ENV INFRA_CONFIG_FILE=/etc/api/infra.json
# Device/asset/sensor/bus registry data comes from Postgres (DATABASE_URL, see
# src/db/client.ts) now, not sensors.json/devices.json/assets.json — nothing to bake in or
# mount for those anymore.
USER bun
EXPOSE 3333/tcp
ENTRYPOINT ["bun", "run", "src/index.ts"]
