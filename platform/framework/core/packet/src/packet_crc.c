/**
 * @file packet_crc.c
 * @brief CRC-16/CCITT-FALSE implementation for the packet module.
 *
 * Algorithm parameters:
 *   - Polynomial : 0x1021
 *   - Init value : 0xFFFF
 *   - Input refl : false
 *   - Output refl: false
 *   - Final XOR  : 0x0000
 *
 * Software (bit-by-bit) implementation — no look-up table.
 * Code size is small; throughput is adequate for the packet sizes used in ZC2X.
 */

#include "packet_crc.h"

/* -------------------------------------------------------------------------
 * Private helpers
 * ---------------------------------------------------------------------- */

#define CRC16_POLY 0x1021U
#define CRC16_INIT 0xFFFFU

/**
 * @brief Process a single byte into the running CRC.
 */
static uint16_t crc16_step(uint16_t crc, uint8_t byte)
{
  crc ^= (uint16_t)((uint16_t)byte << 8U);

  for (int i = 0; i < 8; i++)
  {
    if (crc & 0x8000U)
    {
      crc = (uint16_t)((crc << 1U) ^ CRC16_POLY);
    }
    else
    {
      crc = (uint16_t)(crc << 1U);
    }
  }

  return crc;
}

/* -------------------------------------------------------------------------
 * Public API
 * ---------------------------------------------------------------------- */

uint16_t packet_crc16_compute(const uint8_t *data, size_t length)
{
  uint16_t crc = CRC16_INIT;

  for (size_t i = 0; i < length; i++)
  {
    crc = crc16_step(crc, data[i]);
  }

  return crc;
}

uint16_t packet_crc16_continue(uint16_t crc, const uint8_t *data, size_t length)
{
  for (size_t i = 0; i < length; i++)
  {
    crc = crc16_step(crc, data[i]);
  }

  return crc;
}
