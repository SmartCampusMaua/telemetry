/**
 * @file packet_builder.c
 * @brief zc2x_packet_init() — packet construction.
 */

#include <stddef.h>
#include <string.h>

#include "packet_crc.h"
#include "zc2x_packet.h"

zc2x_result_t zc2x_packet_init(zc2x_packet_t *packet,
                               zc2x_packet_type_t type,
                               const uint8_t device_id[ZC2X_DEVICE_ID_SIZE],
                               uint32_t sequence,
                               uint64_t timestamp,
                               uint32_t can_id,
                               const uint8_t *data,
                               uint8_t dlc)
{
  if (packet == NULL || device_id == NULL)
  {
    return ZC2X_ERR_NULL;
  }

  if (dlc > ZC2X_CAN_DATA_SIZE)
  {
    return ZC2X_ERR_OVERFLOW;
  }

  if (dlc > 0U && data == NULL)
  {
    return ZC2X_ERR_NULL;
  }

  memset(packet, 0, sizeof(*packet));
  packet->type = (uint8_t)type;
  memcpy(packet->device_id, device_id, ZC2X_DEVICE_ID_SIZE);
  packet->sequence = sequence;
  packet->timestamp = timestamp;
  packet->can_id = can_id;
  packet->dlc = dlc;

  if (dlc > 0U)
  {
    memcpy(packet->data, data, dlc);
  }

  uint16_t crc = packet_crc16_compute((const uint8_t *)packet,
                                      offsetof(zc2x_packet_t, crc16));
  packet->crc16 = crc;
  return ZC2X_OK;
}
