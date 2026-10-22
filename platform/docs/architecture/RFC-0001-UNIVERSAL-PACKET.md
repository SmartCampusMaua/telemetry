# RFC-0001 — Universal Packet Specification

| Status       | Draft                    |
| ------------ | ------------------------ |
| RFC          | 0001                     |
| Version      | 1.0                      |
| Last Updated | 2026-07-09               |
| Authors      | Rogério Cassares, OpenAI |
| Applies To   | ZC2X                     |

---

# 1. Abstract

This document defines the Universal Packet Specification used throughout the ZC2X ecosystem.

The Universal Packet is the fundamental unit of communication inside ZC2X.

Every subsystem exchanges information exclusively through packets.

Examples include:

- CAN telemetry
- GNSS measurements
- Diagnostic information
- OTA progress
- System events
- Error reporting
- Storage records
- Wi-Fi uploads
- XBee radio transmission

Once converted into a packet, information becomes transport-independent.

---

# 2. Motivation

Embedded systems traditionally define different message formats for each transport.

For example:

CAN Frame

↓

UART Frame

↓

Radio Frame

↓

HTTP JSON

↓

Database Record

Each conversion introduces additional complexity, code duplication, and failure points.

ZC2X adopts a different philosophy.

Every subsystem exchanges exactly the same packet.

Storage stores packets.

Wi-Fi uploads packets.

XBee transmits packets.

The RSU forwards packets.

The HQ receives packets.

Only the payload producer and payload consumer interpret packet contents.

---

# 3. Design Goals

The packet specification was designed according to the following principles.

## Simplicity

The packet must be simple enough to be generated on low-cost microcontrollers.

## Binary

Packets are binary.

No JSON.

No text.

No XML.

## Transport Independent

Packets must not depend on:

- Wi-Fi
- Ethernet
- UART
- XBee
- HTTP
- MQTT

## Offline First

Packets may remain stored for days before transmission.

## Forward Compatible

Future firmware versions must continue understanding previous packet versions whenever possible.

## Efficient

Header overhead shall remain minimal.

Payload shall occupy most of the packet.

---

# 4. Packet Philosophy

Everything is a Packet.

Packets are immutable after creation.

Packets never change while travelling through the system.

Subsystems may only:

- create packets
- consume packets
- forward packets
- store packets

Subsystems shall never modify packets.

---

# 5. Packet Lifecycle

Packet Created

↓

Published

↓

One or more consumers

↓

Storage

↓

Radio

↓

Wi-Fi

↓

RSU

↓

HQ

↓

Archived

---

# 6. Packet Types

The packet header identifies its payload type.

Examples include:

- CAN Frame
- GNSS Position
- System Event
- Diagnostic
- OTA
- Log
- Future Extensions

The payload format is determined solely by the Packet Type field.

---

# 7. Versioning

Every packet contains a Packet Version field.

Major packet format changes require a new packet version.

Payload formats may evolve independently.

---

# 8. Endianness

All multibyte fields shall use Little Endian encoding.

This matches the native architecture of ESP32 and minimizes processing overhead.

---

# 9. Integrity

Every packet contains a CRC field.

CRC validation occurs before packet processing.

Packets with invalid CRC shall be discarded.

---

# 10. Immutability

Once a packet has been created:

- payload cannot change
- timestamp cannot change
- sequence cannot change
- device identity cannot change

Only transport metadata may exist outside the packet.

Examples include:

- RSSI
- Wi-Fi RSSI
- UART errors
- Reception timestamp

These are transport metadata, not packet contents.

---

# 11. Packet Header

The exact binary layout is defined in a future revision of this RFC.

The packet header will contain:

- Version
- Header Length
- Packet Type
- Flags
- Device Identifier
- Packet Sequence
- Timestamp
- Payload Length
- CRC

---

# 12. Payload

The payload is completely opaque to the transport layer.

Examples:

CAN payload

GNSS payload

OTA payload

Storage payload

The transport layer shall never inspect payload contents.

---

# 13. Packet Size

The packet format shall support variable payload lengths.

The maximum payload depends on the transport.

The packet definition itself does not impose transport-specific limits.

---

# 14. Reserved Fields

Header space shall reserve bits for future protocol evolution.

Reserved fields shall be transmitted as zero.

Receivers shall ignore unknown reserved bits.

---

# 15. Security

Packet authenticity is intentionally outside the scope of this RFC.

Authentication mechanisms may wrap packets without modifying packet contents.

---

# 16. Future Extensions

Possible future extensions include:

- Encryption
- Authentication
- Compression
- Packet Fragmentation
- Packet Aggregation
- Selective Acknowledgements

None of these extensions shall require changing packet payload semantics.

---

# 17. Summary

The Universal Packet is the only data structure exchanged inside ZC2X.

Every subsystem is either:

- a packet producer,
- a packet consumer,
- or a packet forwarder.

This principle forms the foundation of the ZC2X architecture.
