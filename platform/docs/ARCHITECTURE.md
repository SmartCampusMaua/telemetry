# ZC2X Architecture

This document describes the system as it exists in the codebase today. For
the philosophy behind individual design decisions, see the RFCs and ADRs
under `docs/architecture/` and `docs/adr/` (most are still empty — see
[docs/README.md](README.md) for what's written vs. planned).

## 1. Data path

```
ECU (CAN simulator)          OBU                                  RSU                         NATS Core
      │                       │                                    │                              │
      │  CAN frame (TWAI)     │                                    │                              │
      ├──────────────────────>│                                    │                              │
      │                       │  zc2x_packet_t                     │                              │
      │                       ├── XBee (UART1, 115200) ───────────>│                              │
      │                       │                                    │  validate + forward raw bytes │
      │                       │                                    ├─────────────────────────────>│
      │                       │  zc2x_packet_t                     │                               │
      │                       └── WiFi ───────────────────────────────────────────────────────────>│
```

The MVP path, as stated in `.github/copilot-instructions.md`:

```
CAN → OBU → XBee → RSU → NATS Core
```

`ecu/` is a development aid, not part of the deployed system: it simulates a
vehicle ECU by transmitting synthetic CAN frames so OBU/RSU can be exercised
without real vehicle hardware.

OBU publishes to NATS over WiFi **independently** of the XBee path — a
WiFi/NATS outage must not block CAN reception or XBee transmission (this is
stated explicitly in `obu/main/main.c`'s file header and enforced by using a
non-blocking queue send for the NATS path).

## 2. Repository layout

| Path                                                       | Purpose                                                                                                                                                                           | Status                                                                                    |
| ---------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| `platform/esp-idf/firmware/{obu,rsu,ecu}/`                 | The three real, buildable ESP-IDF firmware projects                                                                                                                               | Implemented                                                                               |
| `framework/core/{packet,types}/`                           | Platform-independent shared components, consumed by the firmware via `EXTRA_COMPONENT_DIRS`                                                                                       | Implemented                                                                               |
| `framework/core/{bus,config,logger,time}/`                 | Planned shared components                                                                                                                                                         | Scaffolded only — empty headers/sources, not referenced by any build                      |
| `framework/{hal,protocols,services,transports,utilities}/` | Planned platform-independent modules (HAL wrappers, CAN/NMEA/UBX/protobuf protocols, storage/OTA/web services, WiFi/XBee/HTTP transports, ringbuffer/CRC/base64/endian utilities) | Scaffolded only — directory + README stubs, no code                                       |
| `applications/{obu,rsu,gateways}/`                         | Placeholder for a future application-entry-point layer described in `.github/copilot-instructions.md`                                                                             | Not populated — the real entry points today are `platform/esp-idf/firmware/*/main/main.c` |
| `old/zc2x_*`                                               | First-generation shared component library (`zc2x_config`, `zc2x_core`, `zc2x_event`, `zc2x_logger`, plus several empty directories for `zc2x_can`/`zc2x_wifi`/`zc2x_nats`/etc.)   | Deprecated — superseded by `framework/core/*`, not referenced by any current build        |
| `infra/{nats,docker}/`                                     | Local NATS Core broker (JetStream + WebSocket) for development                                                                                                                    | Implemented (dev infra)                                                                   |
| `docs/`                                                    | RFCs, ADRs, and project documentation                                                                                                                                             | Mostly scaffolded — see [docs/README.md](README.md)                                       |

Per `.github/copilot-instructions.md`: `framework/` must stay platform-independent,
`platform/esp-idf/` holds ESP-IDF-specific code, and platform-specific code must
never be moved into `framework/`.

## 3. Firmware targets

| Target | Role                                                                                                                                     | Target chip | Peripherals                                                       | Depends on                                      |
| ------ | ---------------------------------------------------------------------------------------------------------------------------------------- | ----------- | ----------------------------------------------------------------- | ----------------------------------------------- |
| `obu`  | On-board unit: reads vehicle CAN, forwards each frame as a packet over XBee, and independently republishes it to NATS over WiFi          | `esp32c6`   | TWAI (CAN, 500 kbps, listen-only), UART1 (XBee, 115200), WiFi STA | `framework/core/packet`, `framework/core/types` |
| `rsu`  | Roadside unit: receives packets from OBU over XBee, validates them (CRC-16 + type), and forwards the raw packet bytes to NATS unmodified | `esp32c6`   | UART1 (XBee, 115200), WiFi STA                                    | `framework/core/packet`, `framework/core/types` |
| `ecu`  | CAN traffic simulator for end-to-end testing without a real vehicle                                                                      | `esp32s3`   | TWAI (CAN, active transmitter)                                    | none (emits raw TWAI frames only)               |

All three use the ESP-IDF v6 TWAI driver API (`esp_twai.h` / `esp_twai_onchip.h`)
and FreeRTOS tasks. Each firmware is an independent ESP-IDF project with its
own `CMakeLists.txt`, `sdkconfig`, and `main/` — there is no single top-level
build; each must be built with its own `idf.py set-target` (`esp32c6` for
obu/rsu, `esp32s3` for ecu).

### OBU internals

- `can_init()` — on-chip TWAI node, listen-only, 500 kbps, accepts all IDs.
- `on_can_rx_done()` — ISR callback, pushes received frames to a queue.
- `can_rx_task` — builds a `zc2x_packet_t` from each CAN frame via
  `zc2x_packet_init()`, sends it immediately over XBee, and enqueues it
  (non-blocking) for the NATS task.
- `nats_task` — waits for WiFi, connects to NATS, drains the packet queue and
  publishes each packet to `OBU_NATS_SUBJECT`.

### RSU internals

- `uart_rx_task` — scans UART1 for the 4-byte sync marker `{0xAA,0x55,0xC2,0x58}`
  (self-resynchronizing on noise/reset), reads the fixed-size packet body with
  a deadline-based helper (`uart_read_exact`, needed because the XBee can
  split one logical packet across multiple RF transmissions), then calls
  `zc2x_packet_validate()` before queuing it.
- `nats_task` — same connect/publish/keepalive pattern as OBU, publishing to
  `RSU_NATS_SUBJECT`.

### ECU internals

- `can_init()` — on-chip TWAI node, active transmitter (not listen-only).
- `can_tx_task` — transmits a fixed, configurable CAN frame at a configurable
  rate (`ECU_CAN_MESSAGES_PER_SEC`, default 10 Hz).

## 4. Packet format

Defined in `framework/core/packet/include/zc2x_packet.h` (see also
[docs/architecture/RFC-0001-UNIVERSAL-PACKET.md](architecture/RFC-0001-UNIVERSAL-PACKET.md)
for the design rationale). The MVP implementation is a fixed-size, packed struct:

```c
typedef struct __attribute__((packed)) {
  uint8_t  type;                          // zc2x_packet_type_t
  uint8_t  device_id[6];
  uint32_t sequence;
  uint64_t timestamp;
  uint32_t can_id;
  uint8_t  dlc;
  uint8_t  data[8];
  uint16_t crc16;
} zc2x_packet_t;
```

`zc2x_packet_type_t` defines `CAN_FRAME`, `GNSS`, `SYSEVT`, `DIAGNOSTIC`, `OTA`,
`LOG` — only `CAN_FRAME` is produced/validated by the current firmware; the
rest are forward-looking. `zc2x_packet_init()` builds and CRCs a packet;
`zc2x_packet_validate()` checks type, DLC bound, and CRC-16 (CCITT-FALSE,
poly `0x1021`, init `0xFFFF`). On the wire, OBU prefixes each packet with a
4-byte sync marker (`0xAA 0x55 0xC2 0x58`) before writing it to UART1; RSU
scans for that marker to (re)synchronize.

## 5. NATS integration

There is no shared NATS component — OBU and RSU each embed their own minimal
NATS Core TCP client (`nats_connect`/`nats_publish`/`nats_process` in each
`main.c`, built directly on `lwip/sockets.h`):

1. Connect via TCP, drain the server's `INFO` message.
2. Send `CONNECT {"verbose":false}`.
3. Publish with `PUB <subject> <len>\r\n<payload>\r\n`.
4. Respond to periodic `PING` with `PONG`.

No TLS, no auth, no JetStream, no subscriptions — this is explicitly MVP-only
(stated in `rsu/main.c`'s file header). OBU publishes to `zc2x.can.obu`, RSU
to `zc2x.can.rsu`. `infra/nats/` and `infra/docker/docker-compose.yaml`
stand up a local NATS Core broker (JetStream + WebSocket) for development.

## 6. Implemented vs. planned

| Component                                                                                                             | Status                                                                                                                                                                   |
| --------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `framework/core/packet`                                                                                               | Implemented — packet struct, init/validate, CRC-16                                                                                                                       |
| `framework/core/types`                                                                                                | Partially implemented — only `zc2x_result_t` has content; `zc2x_types.h`, `zc2x_priority.h`, `zc2x_sequence.h`, `zc2x_timestamp.h`, `zc2x_uuid.h` are empty placeholders |
| `framework/core/config`                                                                                               | Planned — empty stub. A working version exists at `old/zc2x_config` (deprecated, not wired into any current build)                                                       |
| `framework/core/logger`                                                                                               | Planned — empty stub. A working version exists at `old/zc2x_logger` (deprecated)                                                                                         |
| `framework/core/bus`                                                                                                  | Planned — empty stub (`CMakeLists.txt` itself is not a valid component registration)                                                                                     |
| `framework/core/time`                                                                                                 | Planned — empty stub                                                                                                                                                     |
| `framework/hal/*`, `framework/protocols/*`, `framework/services/*`, `framework/transports/*`, `framework/utilities/*` | Planned — directory + README scaffolding only, no code                                                                                                                   |
| `applications/*`                                                                                                      | Planned — placeholder for a future application-entry-point layer; not used today                                                                                         |
| Shared NATS client                                                                                                    | Not extracted — OBU and RSU each embed their own copy                                                                                                                    |
| `device_config.h` → `zc2x_config_get()` convention (root README)                                                      | Aspirational — no current firmware calls it; all three read `device_config.h` macros directly in `main.c`                                                                |

## 7. Build status

All three firmware targets build cleanly with ESP-IDF v6.0.2:

| Target | Chip    | Output                         | App partition free |
| ------ | ------- | ------------------------------ | ------------------ |
| `obu`  | esp32c6 | `zc2x_obu.bin` (0xe9000 bytes) | ~9%                |
| `rsu`  | esp32c6 | `zc2x_rsu.bin` (0xe4e50 bytes) | ~11%               |
| `ecu`  | esp32s3 | `zc2x_ecu.bin` (0x2dfd0 bytes) | ~82%               |

See the root [README.md](../README.md) for build/flash instructions.

## 8. CAN2 signals

The full message/signal layout for CAN2 (MoTeC M180 → OBU) — 9 messages, 28
signals, grouped by update-rate tier — is defined in
[architecture/zc2x-can2.dbc](architecture/zc2x-can2.dbc) (standard Vector DBC
format). Messages are named `CD#` (Chassis Dynamics) / `PE#` (Powertrain/
Engine) / `GPS#` per MoTeC's generic dash/logger CAN naming convention,
numbered rather than named after their exact field mix — see each message's
`CM_ BO_` comment in the DBC for what it actually contains. That file is the
source of truth for the `ecu/` simulator and for signal decoding in
`services/input/nats`; don't duplicate its contents here.
