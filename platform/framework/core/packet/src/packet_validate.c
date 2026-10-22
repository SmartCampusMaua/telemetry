/**
 * @file packet_validate.c
 * @brief zc2x_packet_validate() — packet integrity verification.
 */

#include <stddef.h>

#include "packet_crc.h"
#include "zc2x_packet.h"

zc2x_result_t zc2x_packet_validate(const zc2x_packet_t *packet)
{
  if (packet == NULL)
  {
    return ZC2X_ERR_NULL;
  }

  if (packet->type != (uint8_t)ZC2X_PACKET_TYPE_CAN_FRAME)
  {
    return ZC2X_ERR_INVALID_ARG;
  }

  if (packet->dlc > ZC2X_CAN_DATA_SIZE)
  {
    return ZC2X_ERR_OVERFLOW;
  }

  uint16_t expected = packet_crc16_compute((const uint8_t *)packet,
                                           offsetof(zc2x_packet_t, crc16));

  if (expected != packet->crc16)
  {
    return ZC2X_ERR_CRC;
  }

  return ZC2X_OK;
}
