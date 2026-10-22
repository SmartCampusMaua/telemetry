# framework/core/packet

Implements the fixed-size CAN packet used by the ZC2X MVP data path. See
[docs/architecture/RFC-0001-UNIVERSAL-PACKET.md](../../../docs/architecture/RFC-0001-UNIVERSAL-PACKET.md)
for the design rationale.

## Wire struct

```c
typedef struct __attribute__((packed)) {
  uint8_t  type;                  // zc2x_packet_type_t
  uint8_t  device_id[6];
  uint32_t sequence;
  uint64_t timestamp;
  uint32_t can_id;
  uint8_t  dlc;
  uint8_t  data[8];
  uint16_t crc16;
} zc2x_packet_t;
```

`zc2x_packet_type_t`: `CAN_FRAME`, `GNSS`, `SYSEVT`, `DIAGNOSTIC`, `OTA`,
`LOG`. Only `CAN_FRAME` is produced/validated today — the rest are
forward-looking.

## API

- `zc2x_packet_init(packet, type, device_id, sequence, timestamp, can_id, data, dlc)`
  — zero-fills the struct, copies fields, computes and stores the CRC-16.
  Returns `ZC2X_ERR_NULL` for null pointers, `ZC2X_ERR_OVERFLOW` if
  `dlc > 8`.
- `zc2x_packet_validate(packet)` — checks `type == CAN_FRAME`, `dlc <= 8`,
  and recomputes/compares the CRC-16. Returns `ZC2X_ERR_INVALID_ARG` on
  type mismatch, `ZC2X_ERR_CRC` on CRC mismatch.

## CRC

CRC-16/CCITT-FALSE (poly `0x1021`, init `0xFFFF`, no reflection, no final
XOR), bit-by-bit software implementation (`packet_crc.c`, private header —
not exposed outside this component). Both a one-shot
(`packet_crc16_compute`) and incremental (`packet_crc16_continue`, currently
unused) form exist.

## Wire framing (added by the caller, not this component)

OBU prefixes each packet with a 4-byte sync marker before writing it to
UART1: `0xAA 0x55 0xC2 0x58`. RSU scans incoming bytes for that marker to
(re)synchronize before reading the fixed-size packet body.

## Files

- `include/zc2x_packet.h` — public API and struct definition.
- `src/packet_builder.c` — `zc2x_packet_init`.
- `src/packet_validate.c` — `zc2x_packet_validate`.
- `src/packet_crc.c` / `src/packet_crc.h` — CRC-16 implementation (private).
- `src/packet_serializer.c` — currently empty, not compiled (excluded from
  `CMakeLists.txt` `SRCS`); reserved for a future non-packed wire format.

## Dependencies

`framework/core/types` (for `zc2x_result_t`).
