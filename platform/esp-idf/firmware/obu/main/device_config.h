#pragma once

#include "hal/gpio_types.h"

/* -----------------------------------------------------------------------
 * OBU hardware configuration.
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
#define OBU_CAN_TX_PIN GPIO_NUM_4
#define OBU_CAN_RX_PIN GPIO_NUM_5
#define OBU_XBEE_TX_PIN GPIO_NUM_2
#define OBU_XBEE_RX_PIN GPIO_NUM_3
#elif CONFIG_IDF_TARGET_ESP32S3
#error "OBU pins not yet defined for esp32s3 — fill in OBU_CAN_TX_PIN/OBU_CAN_RX_PIN and OBU_XBEE_TX_PIN/OBU_XBEE_RX_PIN for your S3 board wiring, then remove this #error."
#elif CONFIG_IDF_TARGET_ESP32
/* ESP32 secondary target target */
#define OBU_CAN_TX_PIN GPIO_NUM_21
#define OBU_CAN_RX_PIN GPIO_NUM_22
#define OBU_XBEE_TX_PIN GPIO_NUM_16
#define OBU_XBEE_RX_PIN GPIO_NUM_17
#else
#error "Unsupported IDF target for OBU — add a pin block for this chip above."
#endif

#define OBU_CAN_BITRATE 1000000
#define OBU_XBEE_BAUD 115200

/* -----------------------------------------------------------------------
 * WiFi networks, tried in order until one connects. Each entry also carries
 * the NATS server reachable from that network (different networks usually
 * put you on a different subnet, so the NATS broker IP changes with them).
 * Add/remove/reorder entries here — no need to touch main.c or recompile
 * logic, just the config. After OBU_WIFI_MAX_RETRIES_PER_CANDIDATE failed
 * connection attempts on one entry, OBU moves on to the next (wrapping
 * around), and the NATS client follows whichever network is currently
 * connected.
 *
 * Order matters here: team WiFi (in the box, car stationary, clean private
 * RF) is priority 1 — OBU should always prefer it when reachable. Race-track
 * WiFi is priority 2, opportunistic only: on track, at real vehicle speeds
 * (empirically, WiFi association/DHCP repeatedly fails above ~40 km/h —
 * consumer-grade APs aren't built for a moving, RF-congested client), XBee
 * is the primary link, not WiFi — see RSU_WIFI_CREDENTIALS, which is the
 * side actually expected to carry full-rate telemetry off the track. */
#define OBU_WIFI_CREDENTIALS                                     \
  {                                                              \
      {"MauaRacingTeam", "Mauaracing26!", "192.168.0.100"},      \
      {"Roger_Phone", "12345678", "172.20.10.6"},                \
      {"CompreFSAELive👕👚🎥", "Formulive25*", "192.168.80.10"}, \
  }
#define OBU_WIFI_MAX_RETRIES_PER_CANDIDATE 5
/* Delay between WiFi-down checks in nats_task, in ms */
#define OBU_WIFI_WAIT_DELAY_MS 1000
/* If a candidate associates at the WiFi/L2 layer but never completes DHCP
 * within this window (a captive portal, an exhausted DHCP lease pool, or
 * client isolation on a public/event hotspot are all common causes),
 * nats_task forces a disconnect and advances to the next candidate
 * immediately. This is independent of OBU_WIFI_MAX_RETRIES_PER_CANDIDATE:
 * an associated-but-stuck link never fires WIFI_EVENT_STA_DISCONNECTED, so
 * the retry-count logic alone would wait on it forever. */
#define OBU_WIFI_DHCP_TIMEOUT_MS 15000

/* NATS Core server — port and subject are shared across all networks; the
 * server address itself comes from the active entry in OBU_WIFI_CREDENTIALS */
#define OBU_NATS_PORT 4222
#define OBU_NATS_SUBJECT "zc2x.can.obu"
/* Delay before retrying a failed NATS connect, in ms */
#define OBU_NATS_RECONNECT_DELAY_MS 5000
/* nats_task batches packets into a single NATS publish instead of one
 * publish per packet — each publish still costs a full "PUB subject
 * <len>\r\n...\r\n" header plus its own TCP segment/WiFi frame regardless
 * of payload size, and batching also lets nats_task drain s_nats_queue
 * faster during bursts (the slow network I/O happens once per batch, not
 * once per packet). Flushes on whichever comes first: OBU_NATS_BATCH_
 * MAX_PACKETS reached, or no new packet within OBU_NATS_BATCH_MAX_AGE_MS
 * (also the interval nats_task services the PING/PONG keepalive on, when
 * idle). Each packet in a batch is still independently CRC-validated by
 * the consumer (see services/input/nats) — no extra framing needed. */
#define OBU_NATS_BATCH_MAX_PACKETS 16
#define OBU_NATS_BATCH_MAX_AGE_MS 30

/* No OBU_DEVICE_ID here anymore: this unit's 6-byte device identifier is
 * read from the chip's own factory-burned WiFi MAC at boot instead (see
 * main.c's app_main -> esp_read_mac(s_device_id, ESP_MAC_WIFI_STA)). A MAC
 * is already globally unique per chip, so flashing N boards with this exact
 * same firmware image gives N distinct device_ids automatically -- no
 * per-unit constant to hand-edit, and no risk of two boards colliding
 * because someone forgot to bump one (that used to be a real, documented
 * footgun in this exact spot: the previous compile-time constant required
 * remembering to change the last byte per board, and to avoid RSU's own
 * prefix, before every flash of a new unit). */

/* -----------------------------------------------------------------------
 * Tuning — queue/buffer/stack sizing. Bump these if you see "queue full"
 * warnings or task-related crashes/reboots at higher CAN bus load; no
 * other file needs to change.
 * ----------------------------------------------------------------------- */

/* TWAI driver tx queue depth. OBU runs in Normal mode (not listen-only, so
 * it can ACK real bus traffic — see enable_listen_only in can_init()) but
 * the application never calls twai_node_transmit() for data frames, so this
 * is effectively unused; only kept nonzero because the driver requires it. */
#define OBU_CAN_TX_QUEUE_DEPTH 1
/* Depth of the ISR->task queue for received CAN frames */
#define OBU_CAN_RX_QUEUE_LEN 32
/* can_rx_task stack size, in words (4 bytes each on esp32c6) */
#define OBU_CAN_RX_TASK_STACK 6144

/* UART1 (XBee) RX ring buffer size, in bytes */
#define OBU_XBEE_UART_RX_BUF_SIZE 2048
/* UART1 (XBee) TX ring buffer size, in bytes. At 115200 baud and 38
 * bytes/packet (4-byte sync + 34-byte zc2x_packet_t), the wire itself caps
 * out at ~303 packets/sec — a real vehicle CAN bus can sustain that rate
 * or higher continuously, not just in bursts, so this buffer alone cannot
 * fully absorb the mismatch (it only delays, not prevents, the XBee link
 * blocking once demand is at/above ~303/s indefinitely). What actually
 * protects the rest of the system is that the blocking XBee write no
 * longer happens inside can_rx_task at all — see xbee_tx_task in main.c
 * and OBU_XBEE_TX_QUEUE_LEN below. If sustained "XBee-queue drops" show up
 * in the periodic stats log, the wire itself is the bottleneck; raising
 * OBU_XBEE_BAUD (if your XBee modules support it) is the real fix, not a
 * bigger buffer here. */
#define OBU_XBEE_UART_TX_BUF_SIZE 2048

/* Depth of the can_rx_task->xbee_tx_task packet queue. xbee_tx_task is the
 * only thing that ever blocks on the XBee UART write — decoupled from
 * can_rx_task (highest priority) so a saturated XBee link can never again
 * starve nats_task of CPU time on this single-core chip, regardless of how
 * far CAN traffic exceeds the ~303 pkt/sec wire ceiling documented above.
 * Sized for short bursts only; sustained drops here are expected once CAN
 * traffic is at/above that ceiling and are not a bug. */
#define OBU_XBEE_TX_QUEUE_LEN 32
/* xbee_tx_task stack size, in words (4 bytes each on esp32c6) */
#define OBU_XBEE_TX_TASK_STACK 4096

/* Depth of the can_rx_task->nats_task packet queue. Sized to absorb the
 * WiFi association + DHCP + TCP + NATS handshake time at boot (can easily
 * be several seconds) without dropping while CAN keeps flowing — at
 * ECU's 10 Hz test rate, 128 covers ~13 s of connect time. */
#define OBU_NATS_QUEUE_LEN 128
/* nats_task stack size, in words (4 bytes each on esp32c6) */
#define OBU_NATS_TASK_STACK 8192

/* Depth of the ISR->task queue for TWAI bus-error/state-change events */
#define OBU_CAN_DIAG_QUEUE_LEN 16
/* can_diag_task stack size, in words (4 bytes each on esp32c6) */
#define OBU_CAN_DIAG_TASK_STACK 3072

/* How often can_rx_task/nats_task log periodic throughput summaries instead
 * of one line per frame/publish/drop. Per-event logging at sustained CAN
 * rates was blocking the console UART's TX FIFO long enough to starve the
 * IDLE task and trip the watchdog (can_rx_task stuck busy-waiting inside
 * uart_write/uart_tx_char/uart_ll_get_txfifo_len — confirmed by a task_wdt
 * panic trace). Since ESP32-C6 is single-core (CONFIG_FREERTOS_UNICORE),
 * that busy-wait also starved nats_task of any CPU time at all. */
#define OBU_STATS_LOG_INTERVAL_MS 1000
