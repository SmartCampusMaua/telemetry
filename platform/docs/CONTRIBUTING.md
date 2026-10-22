# Contributing

## Source of truth

Before implementing any feature, read the relevant RFC under
`docs/architecture/`. RFCs and ADRs are the source of truth for architecture
— do not invent or redesign architecture ad hoc. Most RFCs/ADRs are still
empty placeholders (see [docs/README.md](README.md)); if you're about to make
a decision one of them should cover, write it there first.

## Workflow

- Every commit should compile. Don't land a commit that breaks the build for
  `obu`, `rsu`, or `ecu`.
- Never generate placeholder code or TODO-only modules — see
  [STYLE_GUIDE.md](STYLE_GUIDE.md).
- Keep `framework/` platform-independent; keep ESP-IDF-specific code under
  `platform/esp-idf/`.

## Building

Each firmware target is an independent ESP-IDF project. See the root
[README.md](../README.md) for exact build/flash commands per target
(`obu`/`rsu` → `esp32c6`, `ecu` → `esp32s3`).

## Current priority

Per `.github/copilot-instructions.md`, the immediate goal is a reliable MVP
for the `CAN → OBU → XBee → RSU → NATS Core` path. Implement only what's
necessary for that path before extending into planned-but-unimplemented
areas (`framework/core/{config,logger,bus,time}`, `framework/{hal,protocols,
services,transports,utilities}`, `applications/`).
