# ZC2X Documentation

Index of project documentation. RFCs and ADRs are the source of truth for
architecture decisions (per `.github/copilot-instructions.md`) — implement
against them, don't invent new architecture ad hoc.

## Written

| Doc                                                                                    | Description                                                                                                                                                                      |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [ARCHITECTURE.md](ARCHITECTURE.md)                                                     | System overview: data path, repo layout, firmware targets, packet format, NATS integration, implemented vs. planned components                                                   |
| [STYLE_GUIDE.md](STYLE_GUIDE.md)                                                       | Coding conventions                                                                                                                                                               |
| [CONTRIBUTING.md](CONTRIBUTING.md)                                                     | Contributor workflow                                                                                                                                                             |
| [architecture/RFC-0001-UNIVERSAL-PACKET.md](architecture/RFC-0001-UNIVERSAL-PACKET.md) | Universal Packet Specification — the design rationale behind `zc2x_packet_t`                                                                                                     |
| [architecture/zc2x-can2.dbc](architecture/zc2x-can2.dbc)                               | CAN2 signal database (Vector DBC format) — message/signal layout MoTeC M1 Tune transmits on CAN2; source of truth for `ecu/` simulator and `services/input/nats` signal decoding |

## Planned (not yet written)

These exist as empty files/titles marking topics to be documented. They
encode decisions (product direction, protocol design) that should be written
deliberately rather than inferred from code, so they're left blank here
rather than guessed at:

- `VISION.md`, `ROADMAP.md`
- `adr/ADR-0001-PACKET-FIRST.md` through `ADR-0006-FRAMEWORK-ARCHITECTURE.md`
- `architecture/RFC-0002-PACKET-BUS.md`, `RFC-0003-TIME.md`,
  `RFC-0004-STORAGE.md`, `RFC-0005-TRANSPORTS.md`,
  `RFC-0006-DEVICE-IDENTITY.md`, `RFC-0007-NETWORK.md`, `RFC-0008-OTA.md`

See [ARCHITECTURE.md §6](ARCHITECTURE.md#6-implemented-vs-planned) for what
each of these areas actually looks like in code today.

# xbee

FROM DEFAULTS

## rsu

HP preamble id: 1
ID network id: 2026
CE Routing/Message Mode: 1
DL Destination Address Low: 0
TO Transmit Options: 40
NI Node Identifier: 1
BD Baud Rate: 7

## obu

HP preamble id: 1
ID network id: 2026
CE Routing/Message Mode: 0
DL Destination Address Low: FFFF
TO Transmit Options: 40
NI Node Identifier: 3
BD Baud Rate: 7
