# framework/core/types

Shared, header-only base types used across ZC2X components. No `SRCS` — this
component only registers `include/` as a public include path.

## Implemented

- `zc2x_result.h` — `zc2x_result_t`, the common return code used throughout
  the framework: `ZC2X_OK`, `ZC2X_ERR_NULL`, `ZC2X_ERR_INVALID_ARG`,
  `ZC2X_ERR_CRC`, `ZC2X_ERR_OVERFLOW`.

## Planned (empty placeholders)

- `zc2x_types.h`
- `zc2x_priority.h`
- `zc2x_sequence.h`
- `zc2x_timestamp.h`
- `zc2x_uuid.h`

These compile fine as empty headers today since nothing includes them yet —
they mark reserved names for types that haven't been designed.
