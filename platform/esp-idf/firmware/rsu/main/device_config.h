#pragma once

#include "hal/gpio_types.h"

/* -----------------------------------------------------------------------
 * RSU hardware and network configuration.
 *
 * Note: RSU has no CAN/TWAI peripheral — it only relays packets it
 * receives from OBU over XBee, so there's no CAN bitrate to configure here.
 *
 * Pins are selected per chip below, based on whichever target you picked
 * with `idf.py set-target` (see scripts/set-target.sh for a friendly
 * wrapper: wrover -> esp32, s3 -> esp32s3, c6mini -> esp32c6). set-target
 * itself can't be driven from this file — it configures the toolchain
 * before the compiler even runs — but once it's set, CONFIG_IDF_TARGET_*
 * is defined automatically by ESP-IDF and picks the right pins here.
 * Add a new #elif branch to support another board; nothing else changes.
 * ----------------------------------------------------------------------- */

#if CONFIG_IDF_TARGET_ESP32C6
/* ESP32-C6 Mini — main target */
#define RSU_XBEE_TX_PIN GPIO_NUM_2
#define RSU_XBEE_RX_PIN GPIO_NUM_3
#elif CONFIG_IDF_TARGET_ESP32S3
#error "RSU pins not yet defined for esp32s3 — fill in RSU_XBEE_TX_PIN/RSU_XBEE_RX_PIN for your S3 board wiring, then remove this #error."
#elif CONFIG_IDF_TARGET_ESP32
/* ESP32 secondary target target */
#define RSU_XBEE_TX_PIN GPIO_NUM_16
#define RSU_XBEE_RX_PIN GPIO_NUM_17
#else
#error "Unsupported IDF target for RSU — add a pin block for this chip above."
#endif

#define RSU_XBEE_BAUD 115200

/* -----------------------------------------------------------------------
 * WiFi networks, tried in order until one connects. Each entry also carries
 * the NATS server reachable from that network (different networks usually
 * put you on a different subnet, so the NATS broker IP changes with them).
 * Add/remove/reorder entries here — no need to touch main.c or recompile
 * logic, just the config. After RSU_WIFI_MAX_RETRIES_PER_CANDIDATE failed
 * connection attempts on one entry, RSU moves on to the next (wrapping
 * around), and the NATS client follows whichever network is currently
 * connected.
 *
 * Order matters here, deliberately the opposite priority from OBU: RSU is
 * stationary at the track edge specifically to relay the XBee link's
 * full-rate on-track telemetry, so race-track WiFi is priority 1. Team WiFi
 * stays as a secondary/opportunistic candidate for when RSU happens to be
 * positioned in/near the box instead. See OBU_WIFI_CREDENTIALS.
 * ----------------------------------------------------------------------- */
#define RSU_WIFI_CREDENTIALS                                     \
  {                                                              \
      {"CompreFSAELive👕👚🎥", "Formulive25*", "192.168.80.10"}, \
      {"MauaRacingTeam", "Mauaracing26!", "192.168.0.100"},      \
      {"Roger_Phone", "12345678", "172.20.10.6"},                \
  }
#define RSU_WIFI_MAX_RETRIES_PER_CANDIDATE 5
/* Delay between WiFi-down checks in nats_task, in ms */
#define RSU_WIFI_WAIT_DELAY_MS 1000
/* If a candidate associates at the WiFi/L2 layer but never completes DHCP
 * within this window (a captive portal, an exhausted DHCP lease pool, or
 * client isolation on a public/event hotspot are all common causes),
 * nats_task forces a disconnect and advances to the next candidate
 * immediately. This is independent of RSU_WIFI_MAX_RETRIES_PER_CANDIDATE:
 * an associated-but-stuck link never fires WIFI_EVENT_STA_DISCONNECTED, so
 * the retry-count logic alone would wait on it forever. */
#define RSU_WIFI_DHCP_TIMEOUT_MS 15000

/* NATS Core server — port and subject are shared across all networks; the
 * server address itself comes from the active entry in RSU_WIFI_CREDENTIALS */
#define RSU_NATS_PORT 4222
#define RSU_NATS_SUBJECT "zc2x.can.rsu"
/* Delay before retrying a failed NATS connect, in ms */
#define RSU_NATS_RECONNECT_DELAY_MS 5000
/* nats_task batches packets into a single NATS publish instead of one
 * publish per packet — each publish still costs a full "PUB subject
 * <len>\r\n...\r\n" header plus its own TCP segment/WiFi frame regardless
 * of payload size, and batching also lets nats_task drain s_packet_queue
 * faster during bursts (the slow network I/O happens once per batch, not
 * once per packet). Flushes on whichever comes first: RSU_NATS_BATCH_
 * MAX_PACKETS reached, or no new packet within RSU_NATS_BATCH_MAX_AGE_MS
 * (also the interval nats_task services the PING/PONG keepalive on, when
 * idle). Each packet in a batch is still independently CRC-validated by
 * the consumer (see services/input/nats) — no extra framing needed. */
#define RSU_NATS_BATCH_MAX_PACKETS 16
#define RSU_NATS_BATCH_MAX_AGE_MS 30

/* No RSU_DEVICE_ID here anymore: this unit's 6-byte device identifier is
 * read from the chip's own factory-burned WiFi MAC at boot instead (see
 * main.c's app_main -> esp_read_mac(s_device_id, ESP_MAC_WIFI_STA)), same
 * as OBU's own device_config.h. A MAC is already globally unique per chip,
 * so flashing N boards with this exact firmware image gives N distinct
 * device_ids automatically -- no per-unit constant to hand-edit. */

/* -----------------------------------------------------------------------
 * Tuning — queue/buffer/stack sizing and UART timeouts. Bump these if you
 * see "queue full" warnings, "incomplete packet" drops, or task-related
 * crashes/reboots at higher packet rates; no other file needs to change.
 * ----------------------------------------------------------------------- */

/* UART1 (XBee) RX ring buffer size, in bytes */
#define RSU_XBEE_UART_RX_BUF_SIZE 4096
/* Depth of the uart_rx_task->nats_task packet queue. Sized to absorb the
 * WiFi association + DHCP + TCP + NATS handshake time at boot (can easily
 * be several seconds) without dropping while packets keep arriving over
 * XBee — at ECU's 10 Hz test rate, 128 covers ~13 s of connect time. */
#define RSU_PACKET_QUEUE_LEN 128
/* uart_rx_task stack size, in words (4 bytes each on esp32c6) */
#define RSU_UART_RX_TASK_STACK 8192
/* nats_task stack size, in words (4 bytes each on esp32c6) */
#define RSU_NATS_TASK_STACK 8192

/* Per-byte read timeout while scanning for the sync marker, in ms */
#define RSU_UART_SYNC_BYTE_TIMEOUT_MS 500
/* Total deadline to read a full packet body once sync is found, in ms —
 * must cover the XBee splitting one logical packet across RF transmissions */
#define RSU_UART_PACKET_READ_TIMEOUT_MS 2000

/* How often uart_rx_task/nats_task log periodic throughput summaries instead
 * of one line per packet/publish/drop (uart_rx_task's log_packet_bytes()
 * alone was two ESP_LOGI calls per packet — a field breakdown plus a full
 * hex dump). Per-event logging at sustained packet rates was blocking the
 * console UART's TX FIFO long enough to starve the IDLE task and trip the
 * watchdog — same root cause as OBU, see OBU_STATS_LOG_INTERVAL_MS. */
#define RSU_STATS_LOG_INTERVAL_MS 1000
