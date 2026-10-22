#pragma once

#include "hal/gpio_types.h"

/* -----------------------------------------------------------------------
 * ECU hardware and simulator configuration.
 *
 * Pins are selected per chip below, based on whichever target you picked
 * with `idf.py set-target` (see scripts/set-target.sh for a friendly
 * wrapper: wrover -> esp32, s3 -> esp32s3, c6mini -> esp32c6). set-target
 * itself can't be driven from this file — it configures the toolchain
 * before the compiler even runs — but once it's set, CONFIG_IDF_TARGET_*
 * is defined automatically by ESP-IDF and picks the right pins here.
 * Add a new #elif branch to support another board; nothing else changes.
 * ----------------------------------------------------------------------- */
#if CONFIG_IDF_TARGET_ESP32S3
#define ECU_CAN_TX_PIN GPIO_NUM_1
#define ECU_CAN_RX_PIN GPIO_NUM_21
#elif CONFIG_IDF_TARGET_ESP32C6
#error "ECU pins not yet defined for esp32c6 — fill in ECU_CAN_TX_PIN/ECU_CAN_RX_PIN for your C6 board wiring, then remove this #error."
#elif CONFIG_IDF_TARGET_ESP32
#error "ECU pins not yet defined for esp32 (Wrover) — fill in ECU_CAN_TX_PIN/ECU_CAN_RX_PIN for your Wrover board wiring, then remove this #error."
#else
#error "Unsupported IDF target for ECU — add a pin block for this chip above."
#endif

/* CAN simulator timing */
#define ECU_CAN_BITRATE 1000000

/* Scheduler tick: the can_tx_task loop period, in ms. This must be the GCD
 * of every ECU_CAN_PERIOD_*_MS below (10 ms here) so each message fires on
 * an exact tick boundary without needing multiple tasks or an LCM scheme. */
#define ECU_CAN_TICK_MS 10U

/* CAN2 message IDs and periods (standard 11-bit IDs). Mirrors
 * docs/architecture/zc2x-can2.dbc — keep both in sync by hand. This is the
 * message/signal layout MoTeC M1 Tune transmits on CAN2 in production; here
 * it's simulated so OBU/RSU/services can be exercised without the car.
 *
 * Names follow MoTeC's generic dash/logger CAN convention: CD# = Chassis
 * Dynamics, PE# = Powertrain/Engine, numbered per message rather than named
 * after their exact field mix (see the DBC's CM_ BO_ comments for what each
 * one actually contains). */
#define ECU_CAN_ID_CD1 0x100U /* wheel speeds */
#define ECU_CAN_PERIOD_CD1_MS 10U /* 100 Hz */

#define ECU_CAN_ID_CD2 0x110U /* steering angle, lateral/longitudinal G, ground speed */
#define ECU_CAN_PERIOD_CD2_MS 10U /* 100 Hz */

#define ECU_CAN_ID_CD3 0x120U /* brake line pressures */
#define ECU_CAN_PERIOD_CD3_MS 10U /* 100 Hz */

#define ECU_CAN_ID_PE1 0x200U /* RPM, throttle, lambda, MAP, gear */
#define ECU_CAN_PERIOD_PE1_MS 20U /* 50 Hz */

#define ECU_CAN_ID_PE2 0x210U /* coolant/oil/intake air temp, oil pressure */
#define ECU_CAN_PERIOD_PE2_MS 100U /* 10 Hz */

#define ECU_CAN_ID_PE3 0x211U /* fuel line pressure, cumulative fuel used */
#define ECU_CAN_PERIOD_PE3_MS 100U /* 10 Hz */

#define ECU_CAN_ID_PE4 0x220U /* per-cylinder exhaust gas temperature */
#define ECU_CAN_PERIOD_PE4_MS 200U /* 5 Hz */

#define ECU_CAN_ID_GPS1 0x300U /* latitude, longitude */
#define ECU_CAN_PERIOD_GPS1_MS 100U /* 10 Hz — matches a typical GPS receiver fix rate; adjust to your actual module */

#define ECU_CAN_ID_GPS2 0x310U /* altitude, GPS-derived ground speed */
#define ECU_CAN_PERIOD_GPS2_MS 100U /* 10 Hz */

/* -----------------------------------------------------------------------
 * Tuning — queue/stack sizing. Bump these if you see TWAI tx timeouts or
 * task-related crashes; no other file needs to change.
 * ----------------------------------------------------------------------- */

/* TWAI driver tx queue depth (frames buffered before twai_node_transmit blocks) */
#define ECU_CAN_TX_QUEUE_DEPTH 8

/* can_tx_task stack size, in words (4 bytes each on esp32s3) */
#define ECU_CAN_TX_TASK_STACK 4096

/* How often can_tx_task logs a periodic queued/failed summary instead of
 * staying silent between boot and the first failure. See OBU_STATS_LOG_
 * INTERVAL_MS for the same reasoning — per-frame logging at up to 300
 * msgs/sec would flood the console. */
#define ECU_STATS_LOG_INTERVAL_MS 1000

