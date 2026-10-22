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

#define ECU_CAN_ID_CD4 0x170U /* central chassis IMU: 3-axis linear G (lateral/longitudinal/vertical) */
#define ECU_CAN_ID_CD5 0x171U /* central chassis IMU: roll/pitch angle (AHRS) */
#define ECU_CAN_ID_CD6 0x172U /* damper position, all 4 corners */
#define ECU_CAN_ID_CD7 0x173U /* central chassis IMU: yaw/roll/pitch rate (gyroscope) */
#define ECU_CAN_PERIOD_CD4_MS 10U /* 100 Hz, matches CD1-3 */
#define ECU_CAN_PERIOD_CD5_MS 10U /* 100 Hz, matches CD4 */
#define ECU_CAN_PERIOD_CD6_MS 10U /* 100 Hz, matches CD1-4 */
#define ECU_CAN_PERIOD_CD7_MS 10U /* 100 Hz, matches CD1-4/CD6 */

#define ECU_CAN_ID_TTUFL 0x130U /* front-left tire surface temp: inner/middle/outer, + pressure */
#define ECU_CAN_PERIOD_TTUFL_MS 100U /* 10 Hz */

#define ECU_CAN_ID_TTUFR 0x140U /* front-right tire surface temp: inner/middle/outer */
#define ECU_CAN_PERIOD_TTUFR_MS 100U /* 10 Hz */

#define ECU_CAN_ID_TTURL 0x150U /* rear-left tire surface temp: inner/middle/outer */
#define ECU_CAN_PERIOD_TTURL_MS 100U /* 10 Hz */

#define ECU_CAN_ID_TTURR 0x160U /* rear-right tire surface temp: inner/middle/outer */
#define ECU_CAN_PERIOD_TTURR_MS 100U /* 10 Hz */

#define ECU_CAN_ID_PE1 0x200U /* RPM, throttle, lambda, MAP, gear */
#define ECU_CAN_PERIOD_PE1_MS 20U /* 50 Hz */

#define ECU_CAN_ID_PE2 0x210U /* coolant/oil/intake air temp, oil pressure */
#define ECU_CAN_PERIOD_PE2_MS 100U /* 10 Hz */

#define ECU_CAN_ID_PE3 0x211U /* fuel line pressure, cumulative fuel used */
#define ECU_CAN_PERIOD_PE3_MS 100U /* 10 Hz */

#define ECU_CAN_ID_PE4 0x220U /* per-cylinder exhaust gas temperature, cylinders 1-4 */
#define ECU_CAN_PERIOD_PE4_MS 200U /* 5 Hz */

#define ECU_CAN_ID_PE5 0x221U /* per-cylinder exhaust gas temperature, cylinders 5-8 */
#define ECU_CAN_PERIOD_PE5_MS 200U /* 5 Hz, matches PE4 */

#define ECU_CAN_ID_GPS1 0x300U /* latitude, longitude */
#define ECU_CAN_PERIOD_GPS1_MS 100U /* 10 Hz — matches a typical GPS receiver fix rate; adjust to your actual module */

#define ECU_CAN_ID_GPS2 0x310U /* altitude, GPS-derived ground speed */
#define ECU_CAN_PERIOD_GPS2_MS 100U /* 10 Hz */

/* PDM30/M150/C125/L180 custom re-aggregation — see the DBC's CM_ BU_
 * comments for each device (docs/architecture/zc2x-can2.dbc) for why these
 * are a custom layout, not a mirror of any device's native CAN broadcast. */
#define ECU_CAN_ID_PD1 0x400U /* PDM: battery voltage, total current, internal temp, error flags */
#define ECU_CAN_PERIOD_PD1_MS 50U /* 20 Hz — matches PDM's confirmed native CAN rate */

#define ECU_CAN_ID_PD2 0x401U /* PDM: output currents 1-8 of 30 */
#define ECU_CAN_ID_PD3 0x402U /* PDM: output currents 9-16 of 30 */
#define ECU_CAN_ID_PD4 0x403U /* PDM: output currents 17-24 of 30 */
#define ECU_CAN_ID_PD5 0x404U /* PDM: output currents 25-30 of 30 */
#define ECU_CAN_PERIOD_PD_CURRENT_MS 50U /* 20 Hz, all 4 output-current messages */

#define ECU_CAN_ID_PD6 0x405U /* PDM: output voltages 1-8 of 30 */
#define ECU_CAN_ID_PD7 0x406U /* PDM: output voltages 9-16 of 30 */
#define ECU_CAN_ID_PD8 0x407U /* PDM: output voltages 17-24 of 30 */
#define ECU_CAN_ID_PD9 0x408U /* PDM: output voltages 25-30 of 30 */
#define ECU_CAN_PERIOD_PD_VOLTAGE_MS 50U /* 20 Hz, all 4 output-voltage messages */

#define ECU_CAN_ID_PD10 0x409U /* PDM: all 30 output on/off states, packed 1 bit each */
#define ECU_CAN_ID_PD11 0x40AU /* PDM: all 16 input states, packed 1 bit each */
#define ECU_CAN_PERIOD_PD_STATE_MS 50U /* 20 Hz, both packed-state messages */

#define ECU_CAN_ID_PD12 0x40BU /* PDM: input voltages 1-4 of 16 */
#define ECU_CAN_ID_PD13 0x40CU /* PDM: input voltages 5-8 of 16 */
#define ECU_CAN_ID_PD14 0x40DU /* PDM: input voltages 9-12 of 16 */
#define ECU_CAN_ID_PD15 0x40EU /* PDM: input voltages 13-16 of 16 */
#define ECU_CAN_PERIOD_PD_INPUT_VOLTAGE_MS 50U /* 20 Hz, all 4 input-voltage messages */

#define ECU_CAN_ID_PD16 0x40FU /* PDM: output loads 1-8 of 30 */
#define ECU_CAN_ID_PD17 0x410U /* PDM: output loads 9-16 of 30 */
#define ECU_CAN_ID_PD18 0x411U /* PDM: output loads 17-24 of 30 */
#define ECU_CAN_ID_PD19 0x412U /* PDM: output loads 25-30 of 30 */
#define ECU_CAN_PERIOD_PD_LOAD_MS 50U /* 20 Hz, all 4 output-load messages */

#define ECU_CAN_ID_EC1 0x440U /* M150: lambda2, ignition timing, ECU battery voltage, barometric pressure */
#define ECU_CAN_ID_EC2 0x441U /* M150: knock level, injector duty cycle, cam angles */
#define ECU_CAN_PERIOD_EC_MS 20U /* 50 Hz, matches PE1's tier */

/* EC3-EC12: real M1-series channel list beyond CD1-3/PE1-4/EC1/EC2 —
 * see the DBC's CM_ BU_ M150 comment (confirmed via AiM InfoTech's MoTeC
 * M1 integration guide, which lists M150 as a supported model). */
#define ECU_CAN_ID_EC3 0x442U /* M150: gearbox/intake/air/ambient temperatures */
#define ECU_CAN_PERIOD_EC3_MS 100U /* 10 Hz, matches PE2's tier */

#define ECU_CAN_ID_EC4 0x443U /* M150: fuel temp, coolant/steering pressure, fuel injection time */
#define ECU_CAN_PERIOD_EC4_MS 100U /* 10 Hz */

#define ECU_CAN_ID_EC5 0x444U /* M150: boost target/actual, engine load average, fuel composition */
#define ECU_CAN_PERIOD_EC5_MS 20U /* 50 Hz, matches PE1's tier — boost is fast-changing */

#define ECU_CAN_ID_EC6 0x445U /* M150: per-bank intake/exhaust cam position */
#define ECU_CAN_ID_EC7 0x446U /* M150: cam position targets + per-bank duty cycle */
#define ECU_CAN_PERIOD_EC_CAM_MS 20U /* 50 Hz, both cam messages */

#define ECU_CAN_ID_EC8 0x447U /* M150: per-cylinder knock level (up to 8 cylinders) */
#define ECU_CAN_ID_EC9 0x448U /* M150: per-cylinder ignition knock trim (up to 8 cylinders) */
#define ECU_CAN_PERIOD_EC_KNOCK_MS 20U /* 50 Hz, both per-cylinder knock messages */

#define ECU_CAN_ID_EC10 0x449U /* M150: output driver levels, run/cut/launch/anti-lag state, gear lever */
#define ECU_CAN_PERIOD_EC10_MS 50U /* 20 Hz — mostly discrete/state signals */

#define ECU_CAN_ID_EC11 0x44AU /* M150: fuel level, ignition time stage, total engine run time */
#define ECU_CAN_PERIOD_EC11_MS 100U /* 10 Hz — all slow-changing */

#define ECU_CAN_ID_EC12 0x44BU /* M150: warning flag bytes (see the DBC's BO_ 1099 comment for values) */
#define ECU_CAN_PERIOD_EC12_MS 50U /* 20 Hz */

#define ECU_CAN_ID_DA1 0x460U /* C125: dash's own 3-axis G sensor */
#define ECU_CAN_PERIOD_DA1_MS 10U /* 100 Hz, matches CD2 for direct comparability */

#define ECU_CAN_ID_DA2 0x461U /* C125: dash temp, sensor supply voltage, dash battery voltage */
#define ECU_CAN_PERIOD_DA2_MS 100U /* 10 Hz — slow-changing relative to DA1 */

#define ECU_CAN_ID_LG1 0x470U /* L180: logger's own 3-axis accelerometer */
#define ECU_CAN_PERIOD_LG1_MS 10U /* 100 Hz, matches CD2/DA1 for direct comparability */

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
