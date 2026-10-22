/**
 * @file main.c
 * @brief RSU firmware entry point.
 *
 * Data path: XBee (UART1) → zc2x_packet_t → WiFi → NATS Core
 *
 * Wire format received on UART1:
 *   [0xAA][0x55][0xC2][0x58][fixed-size packet bytes]
 *
 * The RSU validates each received packet and publishes the raw packet bytes
 * to the NATS subject RSU_NATS_SUBJECT.  Packet contents are never modified.
 *
 * NATS client is a minimal TCP implementation (publish-only, PING/PONG).
 * No JetStream, no TLS, no subscriptions — MVP only.
 */

#include <string.h>
#include <stdio.h>
#include <stdbool.h>
#include <assert.h>
#include <errno.h>

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "freertos/event_groups.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "esp_wifi.h"
#include "esp_mac.h"
#include "esp_event.h"
#include "esp_netif.h"
#include "nvs_flash.h"
#include "driver/uart.h"
#include "lwip/sockets.h"
#include "lwip/netdb.h"

#include "device_config.h"
#include "zc2x_packet.h"

static const char *TAG = "RSU";

/* Populated at boot from this chip's factory-burned WiFi station MAC (see
 * app_main's call to esp_read_mac) -- globally unique per chip already, so
 * flashing N boards with this exact firmware image gives N distinct IDs
 * automatically, no per-unit config to hand-edit. Unlike OBU's own copy of
 * this field, nothing actually reads s_device_id yet: RSU only relays OBU's
 * packet bytes unmodified (RFC-0001), so the device_id field in every
 * packet RSU forwards is always the originating OBU's, never RSU's own --
 * see services/input/nats/internal/signals.go's "origin" comment. Computed
 * and logged anyway so it's visible at boot for operator/diagnostic use
 * now, and ready for whenever a future RSU-originated packet type
 * (PacketTypeDiagnostic/PacketTypeSysEvt already exist in zc2x_packet_t's
 * type enum) needs RSU's own identity. */
static uint8_t s_device_id[ZC2X_DEVICE_ID_SIZE];

/** Total bytes ever read from UART1 (any value). 0 = wir   ing/RF problem. */
static uint32_t s_uart_rx_total;
static bool s_uart_sync_seen;
static QueueHandle_t s_packet_queue;

/* -------------------------------------------------------------------------
 * WiFi
 * ---------------------------------------------------------------------- */

static EventGroupHandle_t s_wifi_event_group;
#define WIFI_CONNECTED_BIT BIT0

/* WiFi candidates: tried in order until one connects (see device_config.h).
 * Each candidate also carries the NATS server reachable on that network. */
typedef struct
{
  const char *ssid;
  const char *password;
  const char *nats_server;
} wifi_credential_t;

static const wifi_credential_t s_wifi_candidates[] = RSU_WIFI_CREDENTIALS;
#define WIFI_CANDIDATE_COUNT \
  (sizeof(s_wifi_candidates) / sizeof(s_wifi_candidates[0]))

static size_t s_wifi_candidate_idx = 0;
static uint32_t s_wifi_fail_count = 0;
/* When we last started trying the current candidate (wifi_apply_candidate())
 * or last confirmed it fully working (IP_EVENT_STA_GOT_IP) — see the DHCP
 * timeout check in nats_task for why this is tracked independently of
 * s_wifi_fail_count. */
static int64_t s_wifi_candidate_started_us = 0;
/* Set by nats_task right before esp_wifi_disconnect() when it decides a
 * candidate is stuck (associated but never got an IP). Consumed by
 * wifi_event_handler's STA_DISCONNECTED handling below, which is the only
 * place that ever calls wifi_apply_candidate()/esp_wifi_connect() in
 * reaction to a disconnect. nats_task must NOT also call those directly:
 * esp_wifi_disconnect() is asynchronous, so its own STA_DISCONNECTED event
 * arrives on the event loop task shortly after — if nats_task raced ahead
 * and called esp_wifi_connect() itself before that event was processed,
 * both ended up calling esp_wifi_connect() within milliseconds of each
 * other and the second one gets rejected ("sta is connecting, return
 * error") — confirmed happening in practice, not theoretical. */
static bool s_wifi_force_advance = false;

static void wifi_apply_candidate(size_t idx)
{
  const wifi_credential_t *cred = &s_wifi_candidates[idx];

  wifi_config_t wifi_cfg = {};
  strncpy((char *)wifi_cfg.sta.ssid, cred->ssid,
          sizeof(wifi_cfg.sta.ssid) - 1);
  strncpy((char *)wifi_cfg.sta.password, cred->password,
          sizeof(wifi_cfg.sta.password) - 1);
  wifi_cfg.sta.threshold.authmode = WIFI_AUTH_WPA2_PSK;

  ESP_ERROR_CHECK(esp_wifi_set_config(WIFI_IF_STA, &wifi_cfg));
  s_wifi_candidate_started_us = esp_timer_get_time();
  ESP_LOGI(TAG, "WiFi trying candidate %u/%u: SSID=%s NATS=%s",
           (unsigned)idx + 1, (unsigned)WIFI_CANDIDATE_COUNT,
           cred->ssid, cred->nats_server);
}

static void wifi_event_handler(void *arg, esp_event_base_t base,
                               int32_t id, void *data)
{
  if (base == WIFI_EVENT && id == WIFI_EVENT_STA_START)
  {
    wifi_apply_candidate(s_wifi_candidate_idx);
    esp_wifi_connect();
  }
  else if (base == WIFI_EVENT && id == WIFI_EVENT_STA_DISCONNECTED)
  {
    /* reason/rssi tell apart AP-side flakiness (BEACON_TIMEOUT, NO_AP_FOUND,
     * weak rssi) from auth/config problems (AUTH_EXPIRE, HANDSHAKE_TIMEOUT,
     * AUTH_FAIL) — see WIFI_REASON_* in esp_wifi_types_generic.h. */
    wifi_event_sta_disconnected_t *ev = (wifi_event_sta_disconnected_t *)data;
    xEventGroupClearBits(s_wifi_event_group, WIFI_CONNECTED_BIT);

    bool force_advance = s_wifi_force_advance;
    s_wifi_force_advance = false;

    s_wifi_fail_count++;
    if (force_advance || s_wifi_fail_count >= RSU_WIFI_MAX_RETRIES_PER_CANDIDATE)
    {
      ESP_LOGW(TAG, "WiFi disconnected (reason=%u rssi=%d) — %s, moving to next candidate",
               (unsigned)ev->reason, (int)ev->rssi,
               force_advance ? "forced (no IP within timeout)" : "giving up on this candidate");
      s_wifi_fail_count = 0;
      s_wifi_candidate_idx = (s_wifi_candidate_idx + 1) % WIFI_CANDIDATE_COUNT;
      wifi_apply_candidate(s_wifi_candidate_idx);
    }
    else
    {
      ESP_LOGW(TAG, "WiFi disconnected (reason=%u rssi=%d) — retrying (%lu/%d)",
               (unsigned)ev->reason, (int)ev->rssi,
               (unsigned long)s_wifi_fail_count,
               RSU_WIFI_MAX_RETRIES_PER_CANDIDATE);
    }
    esp_wifi_connect();
  }
  else if (base == IP_EVENT && id == IP_EVENT_STA_GOT_IP)
  {
    ip_event_got_ip_t *ev = (ip_event_got_ip_t *)data;
    ESP_LOGI(TAG, "WiFi connected — IP " IPSTR, IP2STR(&ev->ip_info.ip));
    s_wifi_fail_count = 0;
    /* Fresh DHCP-timeout budget if this link drops again later — otherwise
     * a link that was happily up for hours would look, to that check, like
     * it's been "stuck" since whenever it first associated. */
    s_wifi_candidate_started_us = esp_timer_get_time();
    xEventGroupSetBits(s_wifi_event_group, WIFI_CONNECTED_BIT);
  }
}

static void wifi_init(void)
{
  s_wifi_event_group = xEventGroupCreate();

  ESP_ERROR_CHECK(esp_netif_init());
  ESP_ERROR_CHECK(esp_event_loop_create_default());
  esp_netif_create_default_wifi_sta();

  wifi_init_config_t cfg = WIFI_INIT_CONFIG_DEFAULT();
  ESP_ERROR_CHECK(esp_wifi_init(&cfg));

  ESP_ERROR_CHECK(esp_event_handler_register(WIFI_EVENT, ESP_EVENT_ANY_ID,
                                             &wifi_event_handler, NULL));
  ESP_ERROR_CHECK(esp_event_handler_register(IP_EVENT, IP_EVENT_STA_GOT_IP,
                                             &wifi_event_handler, NULL));

  ESP_ERROR_CHECK(esp_wifi_set_mode(WIFI_MODE_STA));
  ESP_ERROR_CHECK(esp_wifi_start());

  ESP_LOGI(TAG, "WiFi init done — %u candidate network(s) configured",
           (unsigned)WIFI_CANDIDATE_COUNT);
}

/* -------------------------------------------------------------------------
 * Minimal NATS TCP client
 *
 * Protocol (NATS Core, plain TCP):
 *   ← INFO {...}\r\n
 *   → CONNECT {"verbose":false}\r\n
 *   → PUB <subject> <N>\r\n<N bytes payload>\r\n
 *   ← PING\r\n  (periodic server keepalive)
 *   → PONG\r\n
 * ---------------------------------------------------------------------- */

static int nats_connect(const char *server)
{
  struct sockaddr_in addr = {
      .sin_family = AF_INET,
      .sin_port = htons(RSU_NATS_PORT),
  };
  inet_pton(AF_INET, server, &addr.sin_addr);

  int sock = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
  if (sock < 0)
  {
    ESP_LOGE(TAG, "socket() failed: errno %d", errno);
    return -1;
  }

  /* nats_publish() below issues 3 separate send() calls per packet (header,
   * payload, "\r\n"). With Nagle's algorithm on (the default), those small
   * writes can each wait tens of ms to coalesce with an ACK, capping steady
   * -state throughput well under the CAN frame rate and backing up
   * s_packet_queue even once "connected". TCP_NODELAY sends each write
   * immediately instead. */
  int nodelay = 1;
  if (setsockopt(sock, IPPROTO_TCP, TCP_NODELAY, &nodelay, sizeof(nodelay)) != 0)
  {
    ESP_LOGW(TAG, "setsockopt(TCP_NODELAY) failed: errno %d", errno);
  }

  if (connect(sock, (struct sockaddr *)&addr, sizeof(addr)) != 0)
  {
    ESP_LOGE(TAG, "connect() to %s:%d failed: errno %d",
             server, RSU_NATS_PORT, errno);
    close(sock);
    return -1;
  }

  /* Drain the INFO message sent by the server on connect */
  char info_buf[512];
  int n = recv(sock, info_buf, sizeof(info_buf) - 1, 0);
  if (n <= 0)
  {
    ESP_LOGE(TAG, "recv INFO failed");
    close(sock);
    return -1;
  }
  info_buf[n] = '\0';
  ESP_LOGI(TAG, "NATS server: %.80s", info_buf);

  /* Send CONNECT with verbose disabled to suppress +OK responses */
  const char *connect_msg = "CONNECT {\"verbose\":false}\r\n";
  if (send(sock, connect_msg, strlen(connect_msg), 0) < 0)
  {
    ESP_LOGE(TAG, "send CONNECT failed");
    close(sock);
    return -1;
  }

  ESP_LOGI(TAG, "Connected to NATS %s:%d", server, RSU_NATS_PORT);
  return sock;
}

/**
 * @brief Publish binary data to a NATS subject.
 *
 * @return 0 on success, -1 on send error (caller should reconnect).
 */
/* Publishes 1..RSU_NATS_BATCH_MAX_PACKETS packets as a single NATS message:
 * the payload is just the packets concatenated back-to-back (each is still
 * independently CRC-validated by the consumer, so no extra framing is
 * needed — see services/input/nats). Single buffered send, same reasoning
 * as before: with TCP_NODELAY set (see nats_connect), every separate
 * send() reliably becomes its own TCP segment/WiFi frame with its own
 * fixed airtime overhead, so batching the *publish* is pointless if we
 * then split it back into multiple sends. Payload is binary (may contain
 * embedded nulls), so it's memcpy'd, never touched by a string function. */
static int nats_publish(int sock, const char *subject,
                        const zc2x_packet_t *packets, size_t count)
{
  uint8_t buf[64 + (RSU_NATS_BATCH_MAX_PACKETS * sizeof(zc2x_packet_t)) + 2];
  size_t payload_len = count * sizeof(zc2x_packet_t);

  int hlen = snprintf((char *)buf, 64, "PUB %s %zu\r\n", subject, payload_len);
  if (hlen < 0 || (size_t)hlen >= 64 || (size_t)hlen + payload_len + 2 > sizeof(buf))
  {
    return -1;
  }

  memcpy(buf + hlen, packets, payload_len);
  buf[hlen + payload_len] = '\r';
  buf[hlen + payload_len + 1] = '\n';

  size_t total = (size_t)hlen + payload_len + 2;
  size_t sent = 0;
  while (sent < total)
  {
    ssize_t n = send(sock, buf + sent, total - sent, 0);
    if (n <= 0)
    {
      return -1;
    }
    sent += (size_t)n;
  }

  return 0;
}

/**
 * @brief Drain pending data from the NATS socket and respond to PING.
 *
 * Called after each publish to keep the connection alive.
 *
 * @return 0 on success (including "nothing pending right now"), -1 if the
 * socket looks dead (caller should reconnect) — matches OBU's nats_process()
 * exactly; this used to be void and unchecked here, meaning RSU's keepalive
 * path (as opposed to its publish path, which was already checked) couldn't
 * detect a silently-dead socket until the next actual publish failed.
 */
static int nats_process(int sock)
{
  char buf[128];
  ssize_t n = recv(sock, buf, sizeof(buf) - 1, MSG_DONTWAIT);
  if (n > 0)
  {
    buf[n] = '\0';
    if (strstr(buf, "PING") != NULL)
    {
      if (send(sock, "PONG\r\n", 6, 0) < 0)
      {
        return -1;
      }
    }
    return 0;
  }

  if (n == 0)
  {
    return -1;
  }

  if (errno == EWOULDBLOCK || errno == EAGAIN)
  {
    return 0;
  }

  return -1;
}

/* -------------------------------------------------------------------------
 * UART1 (XBee) initialisation
 * ---------------------------------------------------------------------- */

static void xbee_init(void)
{
  const uart_config_t cfg = {
      .baud_rate = RSU_XBEE_BAUD,
      .data_bits = UART_DATA_8_BITS,
      .parity = UART_PARITY_DISABLE,
      .stop_bits = UART_STOP_BITS_1,
      .flow_ctrl = UART_HW_FLOWCTRL_DISABLE,
      .source_clk = UART_SCLK_DEFAULT,
  };

  /* Install driver first — ESP-IDF applies pin routing after install. */
  ESP_ERROR_CHECK(uart_driver_install(UART_NUM_1, RSU_XBEE_UART_RX_BUF_SIZE,
                                      0, 0, NULL, 0));
  ESP_ERROR_CHECK(uart_param_config(UART_NUM_1, &cfg));
  ESP_ERROR_CHECK(uart_set_pin(UART_NUM_1,
                               RSU_XBEE_TX_PIN, RSU_XBEE_RX_PIN,
                               UART_PIN_NO_CHANGE, UART_PIN_NO_CHANGE));

  ESP_LOGI(TAG, "UART1 (XBee) ready: TX=%d RX=%d baud=%d",
           RSU_XBEE_TX_PIN, RSU_XBEE_RX_PIN, RSU_XBEE_BAUD);
  ESP_LOGI(TAG,
           "XBee DOUT must reach GPIO%d. XCTU on this XBee does NOT loop "
           "sent bytes back to the ESP32 — use a second radio or wire a USB "
           "serial adapter TX directly to GPIO%d for a local test.",
           RSU_XBEE_RX_PIN, RSU_XBEE_RX_PIN);
}

/* -------------------------------------------------------------------------
 * uart_read_exact — read exactly 'len' bytes with a total deadline.
 *
 * WHY: uart_read_bytes() applies its timeout per ring-buffer chunk, not
 * to the total requested length.  When the XBee splits a single logical
 * packet across two or more RF transmissions (e.g. 15 bytes arrive, then
 * 26 bytes arrive 200 ms later), a plain uart_read_bytes(26, 500ms) may
 * return after only 15 bytes, treating the rest as a new unrelated stream.
 *
 * This helper loops, accumulating bytes, until 'len' bytes are collected
 * or 'total_ms' milliseconds have elapsed from the first call.
 *
 * Returns the number of bytes actually read (== len on success).
 * ---------------------------------------------------------------------- */
static int uart_read_exact(uint8_t *buf, int len, int total_ms)
{
  int received = 0;
  TickType_t deadline = xTaskGetTickCount() + pdMS_TO_TICKS(total_ms);

  while (received < len)
  {
    TickType_t now = xTaskGetTickCount();
    TickType_t remaining = (deadline > now) ? (deadline - now) : 0;
    if (remaining == 0)
      break;

    int n = uart_read_bytes(UART_NUM_1, buf + received,
                            (uint32_t)(len - received), remaining);
    if (n > 0)
      received += n;
  }
  return received;
}

static void log_packet_bytes(const zc2x_packet_t *pkt)
{
  const uint8_t *raw = (const uint8_t *)pkt;
  char hex[(sizeof(zc2x_packet_t) * 3U) + 1U];
  size_t pos = 0;

  for (size_t i = 0; i < sizeof(zc2x_packet_t) && pos + 3U < sizeof(hex); ++i)
  {
    int written = snprintf(&hex[pos], sizeof(hex) - pos, "%02X%s",
                           raw[i], (i + 1U < sizeof(zc2x_packet_t)) ? " " : "");
    if (written < 0)
    {
      break;
    }
    pos += (size_t)written;
  }

  hex[sizeof(hex) - 1U] = '\0';

  ESP_LOGI(TAG,
           "RX pkt fields: type=%u device_id=%02X %02X %02X %02X %02X %02X seq=%lu ts=%llu can_id=0x%08lx dlc=%u crc16=0x%04x",
           (unsigned)pkt->type,
           pkt->device_id[0], pkt->device_id[1], pkt->device_id[2],
           pkt->device_id[3], pkt->device_id[4], pkt->device_id[5],
           (unsigned long)pkt->sequence,
           (unsigned long long)pkt->timestamp,
           (unsigned long)pkt->can_id,
           (unsigned)pkt->dlc,
           (unsigned)pkt->crc16);

  ESP_LOGI(TAG, "RX pkt bytes: %s", hex);
}

/* -------------------------------------------------------------------------
 * UART receive task
 * ---------------------------------------------------------------------- */

static void uart_rx_task(void *arg)
{
  zc2x_packet_t pkt;
  zc2x_packet_t sample_pkt;
  bool have_sample = false;
  uint32_t packets_since_log = 0;
  uint32_t drops_since_log = 0;
  uint32_t invalid_since_log = 0;
  uint32_t resyncs_since_log = 0;
  uint32_t resync_bytes_since_log = 0;
  int64_t last_log_us = esp_timer_get_time();

  while (1)
  {
    /* Periodic summary instead of two ESP_LOGI calls per packet — see
     * RSU_STATS_LOG_INTERVAL_MS in device_config.h for why. Checked at the
     * top of the loop so it still fires even while idle/re-syncing. Sync
     * re-alignment is folded in here too rather than logged immediately:
     * at sustained high packet rates, a per-resync ESP_LOGW would itself
     * become a hot-path log flooding the console, the same failure mode
     * this whole change was meant to fix. */
    int64_t now = esp_timer_get_time();
    if (now - last_log_us >= (int64_t)RSU_STATS_LOG_INTERVAL_MS * 1000)
    {
      ESP_LOGI(TAG, "stats: %lu packets, %lu queue drops, %lu invalid, "
                    "%lu resyncs (%lu spurious bytes) in last %d ms",
               (unsigned long)packets_since_log, (unsigned long)drops_since_log,
               (unsigned long)invalid_since_log, (unsigned long)resyncs_since_log,
               (unsigned long)resync_bytes_since_log, RSU_STATS_LOG_INTERVAL_MS);
      if (have_sample)
      {
        log_packet_bytes(&sample_pkt);
        have_sample = false;
      }
      packets_since_log = 0;
      drops_since_log = 0;
      invalid_since_log = 0;
      resyncs_since_log = 0;
      resync_bytes_since_log = 0;
      last_log_us = now;
    }

    /* --- Step 1: scan for sync marker {0xAA, 0x55, 0xC2, 0x58} then read header ---
     *
     * Read byte-by-byte until the 4-byte marker is found.  This guarantees
     * byte alignment regardless of startup noise, XBee resets, or any
     * mid-packet disruption on the radio link.
     * -------------------------------------------------------------------*/
    {
      uint8_t b;
      uint8_t sync[4] = {0};
      uint32_t skipped = 0;
      uint32_t idle_reports = 0;

      while (1)
      {
        int nr = uart_read_bytes(UART_NUM_1, &b, 1,
                                 pdMS_TO_TICKS(RSU_UART_SYNC_BYTE_TIMEOUT_MS));
        if (nr != 1)
        {
          idle_reports++;
          if (idle_reports % 6 == 0) /* every 6 * RSU_UART_SYNC_BYTE_TIMEOUT_MS */
          {
            size_t buffered = 0;
            uart_get_buffered_data_len(UART_NUM_1, &buffered);
            ESP_LOGI(TAG,
                     "UART1 alive — waiting for sync {0xAA 0x55 0xC2 0x58} "
                     "(rx_total=%lu, skipped=%lu, buffered=%u, idle=%lu x 500ms)",
                     (unsigned long)s_uart_rx_total,
                     (unsigned long)skipped,
                     (unsigned)buffered,
                     (unsigned long)idle_reports);
          }
          vTaskDelay(pdMS_TO_TICKS(1));
          continue;
        }

        idle_reports = 0;
        s_uart_rx_total++;

        sync[0] = sync[1];
        sync[1] = sync[2];
        sync[2] = sync[3];
        sync[3] = b;

        if (sync[0] == 0xAAu && sync[1] == 0x55u &&
            sync[2] == 0xC2u && sync[3] == 0x58u)
        {
          break; /* sync marker found */
        }
        skipped++;
      }

      if (skipped > 0)
      {
        if (s_uart_sync_seen)
        {
          resyncs_since_log++;
          resync_bytes_since_log += skipped;
        }
        else
        {
          ESP_LOGI(TAG, "UART sync aligned after %lu startup bytes",
                   (unsigned long)skipped);
          s_uart_sync_seen = true;
        }
      }
    }

    /* Read the fixed packet body — deadline covers XBee splits */
    int n = uart_read_exact((uint8_t *)&pkt, (int)sizeof(zc2x_packet_t),
                            RSU_UART_PACKET_READ_TIMEOUT_MS);

    if (n != (int)sizeof(zc2x_packet_t))
    {
      ESP_LOGW(TAG, "incomplete packet after sync (%d/%u) -- discarding",
               n, (unsigned)sizeof(zc2x_packet_t));
      vTaskDelay(pdMS_TO_TICKS(1));
      continue;
    }

    /* --- Step 2: validate the packet --- */
    zc2x_result_t res = zc2x_packet_validate(&pkt);
    if (res != ZC2X_OK)
    {
      invalid_since_log++;
      continue;
    }

    packets_since_log++;
    sample_pkt = pkt;
    have_sample = true;

    if (xQueueSend(s_packet_queue, &pkt, 0) != pdTRUE)
    {
      drops_since_log++;
    }
  }
}

/* -------------------------------------------------------------------------
 * NATS publish task
 * ---------------------------------------------------------------------- */

static void nats_task(void *arg)
{
  int nats_sock = -1;
  zc2x_packet_t batch[RSU_NATS_BATCH_MAX_PACKETS];
  size_t batch_count = 0;
  uint32_t published_since_log = 0;
  uint32_t batches_since_log = 0;
  int64_t last_log_us = esp_timer_get_time();

  while (1)
  {
    /* Periodic summary instead of a log line per publish — see
     * RSU_STATS_LOG_INTERVAL_MS in device_config.h for why. Checked at the
     * top of the loop so it still fires during WiFi/NATS outages. wifi/sock
     * state is included directly so a stuck "0 publishes" period doesn't
     * require correlating against wifi_event_handler's own log lines to
     * tell apart "no WiFi" from "WiFi up, NATS itself not connecting". */
    int64_t now = esp_timer_get_time();
    if (now - last_log_us >= (int64_t)RSU_STATS_LOG_INTERVAL_MS * 1000)
    {
      bool wifi_up = (xEventGroupGetBits(s_wifi_event_group) & WIFI_CONNECTED_BIT) != 0;
      ESP_LOGI(TAG, "stats: %lu NATS publishes in %lu batches in last %d ms "
                    "(wifi=%s candidate=%s sock=%s)",
               (unsigned long)published_since_log, (unsigned long)batches_since_log,
               RSU_STATS_LOG_INTERVAL_MS, wifi_up ? "up" : "down",
               s_wifi_candidates[s_wifi_candidate_idx].ssid,
               nats_sock >= 0 ? "connected" : "disconnected");
      published_since_log = 0;
      batches_since_log = 0;
      last_log_us = now;
    }

    EventBits_t wifi_bits = xEventGroupGetBits(s_wifi_event_group);
    if ((wifi_bits & WIFI_CONNECTED_BIT) == 0)
    {
      if (nats_sock >= 0)
      {
        close(nats_sock);
        nats_sock = -1;
      }
      batch_count = 0; /* link is down anyway — no point holding a partial batch */

      /* A candidate can associate at the WiFi/L2 layer (see the driver's own
       * "wifi:connected with <ssid>..." log line) and then simply never
       * complete DHCP — a captive portal, an exhausted lease pool, or client
       * isolation on a public/event hotspot are all common causes. That
       * state never fires WIFI_EVENT_STA_DISCONNECTED, so
       * RSU_WIFI_MAX_RETRIES_PER_CANDIDATE's disconnect-counting logic can
       * wait on it forever. This is an independent, time-based escape
       * hatch: if we've been on this candidate (associating or associated)
       * for too long without ever getting an IP, force a disconnect and
       * let wifi_event_handler's STA_DISCONNECTED case (the single owner
       * of candidate-switching/reconnecting) advance to the next candidate
       * — do NOT call wifi_apply_candidate()/esp_wifi_connect() here too;
       * esp_wifi_disconnect() is async and doing so races the event
       * handler's own reaction to the same disconnect. */
      if (!s_wifi_force_advance &&
          esp_timer_get_time() - s_wifi_candidate_started_us >=
              (int64_t)RSU_WIFI_DHCP_TIMEOUT_MS * 1000)
      {
        ESP_LOGW(TAG, "WiFi candidate %s: no IP after %d ms (associated but stuck? "
                      "captive portal / DHCP issue) — forcing disconnect+advance",
                 s_wifi_candidates[s_wifi_candidate_idx].ssid, RSU_WIFI_DHCP_TIMEOUT_MS);
        s_wifi_force_advance = true;
        esp_wifi_disconnect();
      }

      vTaskDelay(pdMS_TO_TICKS(RSU_WIFI_WAIT_DELAY_MS));
      continue;
    }

    if (nats_sock < 0)
    {
      const char *server = s_wifi_candidates[s_wifi_candidate_idx].nats_server;
      nats_sock = nats_connect(server);
      if (nats_sock < 0)
      {
        ESP_LOGW(TAG, "NATS connect to %s failed — retrying in %d ms",
                 server, RSU_NATS_RECONNECT_DELAY_MS);
        vTaskDelay(pdMS_TO_TICKS(RSU_NATS_RECONNECT_DELAY_MS));
        continue;
      }
    }

    /* Accumulate into batch[]; flush on whichever comes first: batch full,
     * or no new packet within RSU_NATS_BATCH_MAX_AGE_MS (bounds worst-case
     * publish latency during low/sporadic traffic). This also drains
     * s_packet_queue faster during bursts, since the slow network I/O now
     * happens once per batch instead of once per packet. */
    bool got_packet = xQueueReceive(s_packet_queue, &batch[batch_count],
                                    pdMS_TO_TICKS(RSU_NATS_BATCH_MAX_AGE_MS)) == pdTRUE;
    if (got_packet)
    {
      batch_count++;
    }

    if (batch_count == 0)
    {
      if (nats_process(nats_sock) != 0)
      {
        ESP_LOGW(TAG, "NATS keepalive failed — reconnecting");
        close(nats_sock);
        nats_sock = -1;
      }
      continue;
    }

    if (got_packet && batch_count < RSU_NATS_BATCH_MAX_PACKETS)
    {
      continue; /* keep accumulating */
    }

    if (nats_publish(nats_sock, RSU_NATS_SUBJECT, batch, batch_count) != 0)
    {
      ESP_LOGE(TAG, "NATS publish failed — reconnecting");
      close(nats_sock);
      nats_sock = -1;
      batch_count = 0;
      continue;
    }

    published_since_log += (uint32_t)batch_count;
    batches_since_log++;
    batch_count = 0;

    if (nats_process(nats_sock) != 0)
    {
      ESP_LOGW(TAG, "NATS keepalive failed — reconnecting");
      close(nats_sock);
      nats_sock = -1;
    }
  }
}

/* -------------------------------------------------------------------------
 * Entry point
 * ---------------------------------------------------------------------- */

void app_main(void)
{
  ESP_LOGI(TAG, "ZC2X RSU starting");

  /* esp_read_mac needs no WiFi/BT driver initialized -- it just derives the
   * requested interface's MAC from the factory-burned base MAC in eFuse --
   * so this is safe to call before wifi_init() below. */
  ESP_ERROR_CHECK(esp_read_mac(s_device_id, ESP_MAC_WIFI_STA));
  ESP_LOGI(TAG, "device_id (WiFi STA MAC): %02x%02x%02x%02x%02x%02x",
           s_device_id[0], s_device_id[1], s_device_id[2],
           s_device_id[3], s_device_id[4], s_device_id[5]);

  /* NVS must be initialised before WiFi */
  esp_err_t ret = nvs_flash_init();
  if (ret == ESP_ERR_NVS_NO_FREE_PAGES ||
      ret == ESP_ERR_NVS_NEW_VERSION_FOUND)
  {
    ESP_ERROR_CHECK(nvs_flash_erase());
    ret = nvs_flash_init();
  }
  ESP_ERROR_CHECK(ret);

  s_packet_queue = xQueueCreate(RSU_PACKET_QUEUE_LEN, sizeof(zc2x_packet_t));
  assert(s_packet_queue != NULL);

  xbee_init();
  wifi_init();

  xTaskCreate(uart_rx_task, "uart_rx_task", RSU_UART_RX_TASK_STACK, NULL,
              configMAX_PRIORITIES - 2, NULL);
  xTaskCreate(nats_task, "nats_task", RSU_NATS_TASK_STACK, NULL,
              configMAX_PRIORITIES - 3, NULL);
}