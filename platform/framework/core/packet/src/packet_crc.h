/**
 * @file packet_crc.h
 * @brief Internal CRC-16/CCITT-FALSE helpers for the packet module.
 *
 * Not part of the public API.  Include only from within framework/core/packet/src/.
 */

#pragma once

#include <stdint.h>
#include <stddef.h>

/**
 * @brief Compute CRC-16/CCITT-FALSE over a data buffer.
 *
 * Algorithm: polynomial 0x1021, init 0xFFFF, no input/output reflection,
 * no final XOR.
 *
 * @param data   Pointer to the first byte.
 * @param length Number of bytes to process.
 *
 * @return 16-bit CRC value.
 */
uint16_t packet_crc16_compute(const uint8_t *data, size_t length);

/**
 * @brief Continue a CRC-16/CCITT-FALSE computation from an existing CRC state.
 *
 * Allows the CRC to be computed over non-contiguous regions (e.g. header
 * bytes followed by payload bytes) without copying them into a single buffer.
 *
 * @param crc    Running CRC value (start with 0xFFFF for a fresh computation).
 * @param data   Pointer to the next data block.
 * @param length Number of bytes to process.
 *
 * @return Updated CRC value.
 */
uint16_t packet_crc16_continue(uint16_t crc, const uint8_t *data, size_t length);
