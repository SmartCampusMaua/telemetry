/**
 * @file zc2x_packet.h
 * @brief Fixed-size CAN packet used by the ZC2X MVP path.
 *
 * The transport frame is:
 *   [sync bytes] [packed packet bytes]
 *
 * The packet itself is fixed-size and contains only CAN frame data.
 * No version, header length, or payload length fields are transmitted.
 */

#pragma once

#include <stddef.h>
#include <stdint.h>

#include "zc2x_result.h"

#ifdef __cplusplus
extern "C"
{
#endif

#define ZC2X_DEVICE_ID_SIZE 6U
#define ZC2X_CAN_DATA_SIZE 8U

  typedef enum
  {
    ZC2X_PACKET_TYPE_INVALID = 0x00,
    ZC2X_PACKET_TYPE_CAN_FRAME = 0x01,
    ZC2X_PACKET_TYPE_GNSS = 0x02,
    ZC2X_PACKET_TYPE_SYSEVT = 0x03,
    ZC2X_PACKET_TYPE_DIAGNOSTIC = 0x04,
    ZC2X_PACKET_TYPE_OTA = 0x05,
    ZC2X_PACKET_TYPE_LOG = 0x06,
  } zc2x_packet_type_t;

  typedef struct __attribute__((packed))
  {
    uint8_t type;
    uint8_t device_id[ZC2X_DEVICE_ID_SIZE];
    uint32_t sequence;
    uint64_t timestamp;
    uint32_t can_id;
    uint8_t dlc;
    uint8_t data[ZC2X_CAN_DATA_SIZE];
    uint16_t crc16;
  } zc2x_packet_t;

  zc2x_result_t zc2x_packet_init(zc2x_packet_t *packet,
                                 zc2x_packet_type_t type,
                                 const uint8_t device_id[ZC2X_DEVICE_ID_SIZE],
                                 uint32_t sequence,
                                 uint64_t timestamp,
                                 uint32_t can_id,
                                 const uint8_t *data,
                                 uint8_t dlc);

  zc2x_result_t zc2x_packet_validate(const zc2x_packet_t *packet);

#ifdef __cplusplus
}
#endif