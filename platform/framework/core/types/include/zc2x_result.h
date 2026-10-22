/**
 * @file zc2x_result.h
 * @brief Common result codes shared across all ZC2X modules.
 */

#pragma once

#ifdef __cplusplus
extern "C"
{
#endif

  /**
   * @brief Return codes used throughout the ZC2X framework.
   */
  typedef enum
  {
    ZC2X_OK = 0,              /**< Success */
    ZC2X_ERR_NULL = 1,        /**< Null pointer argument */
    ZC2X_ERR_INVALID_ARG = 2, /**< Invalid argument value */
    ZC2X_ERR_CRC = 3,         /**< CRC mismatch */
    ZC2X_ERR_OVERFLOW = 4,    /**< Value exceeds allowed maximum */
  } zc2x_result_t;

#ifdef __cplusplus
}
#endif
