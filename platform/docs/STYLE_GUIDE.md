# Style Guide

Derived from `.github/copilot-instructions.md`. This is the same guidance
given to any contributor (human or AI) working on this codebase.

## Language and platform

- C99.
- ESP-IDF (FreeRTOS-based) for all firmware.

## Code

- Keep functions small.
- Keep modules independent.
- Avoid unnecessary abstractions.
- Avoid dynamic allocation unless required.
- Prefer explicit code over clever code.
- Never generate placeholder code. Never generate TODO-only modules.
- Every commit should compile.
- Every module should have a single responsibility.

## Repository organization

- `framework/` — platform-independent code. Must never depend on ESP-IDF.
- `platform/esp-idf/` — ESP-IDF-specific implementation.
- `applications/` — firmware entry points (planned; not populated yet — the
  real entry points today live under `platform/esp-idf/firmware/*/main/`).
- Never move platform-specific code into `framework/`.

## Configuration

Only `main.c` should include `device_config.h`. Every other component should
obtain configuration through a runtime config object rather than compile-time
macros, to keep configuration centralized. (Aspirational — see
[ARCHITECTURE.md §6](ARCHITECTURE.md#6-implemented-vs-planned): no firmware
follows this yet.)

## Packet philosophy

Everything is a packet (see
[RFC-0001](architecture/RFC-0001-UNIVERSAL-PACKET.md)). Packets are immutable
after creation; subsystems may create, consume, forward, or store packets,
but never modify them in transit.

## Priority order for new work

Per copilot-instructions, implement only what's necessary for the MVP, in
this order: CAN reception → packet creation → XBee transmission → RSU
reception → NATS Core publishing. Everything else comes later.
