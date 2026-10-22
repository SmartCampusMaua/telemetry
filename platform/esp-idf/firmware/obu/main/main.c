/**
 * @file main.c
 * @brief OBU firmware entry point.
 *
 * Data path:
 *   CAN (TWAI) -> zc2x_packet_t -> XBee (UART1)
 *   CAN (TWAI) -> zc2x_packet_t -> WiFi -> NATS Core
 *
 * XBee and NATS paths are independent: a WiFi/NATS outage must not block
 * CAN reception or XBee transmission.
 */

#include <assert.h>
#include <string.h>
#include <stdio.h>
#include <errno.h>

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "freertos/queue.h"
#include "freertos/event_groups.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "esp_task_wdt.h"
#include "esp_wifi.h"
#include "esp_mac.h"
#include "esp_event.h"
#include "esp_netif.h"
#include "nvs_flash.h"
#include "driver/uart.h"
#include "lwip/sockets.h"
#include "lwip/netdb.h"
#include "esp_twai.h"
#include "esp_twai_onchip.h"

#include "device_config.h"
#include "zc2x_packet.h"

static const char *TAG = "OBU";

typedef struct __attribute__((packed))
{
  uint32_t id;
  uint8_t dlc;
  uint8_t data[8];
} zc2x_can_payload_t;

typedef struct
{
  uint32_t id;
  uint8_t dlc;
  uint8_t data[8];
} can_rx_frame_t;

/* Populated at boot from this chip's factory-burned WiFi station MAC (see
 * app_main's call to esp_read_mac) -- not a compile-time constant anymore.
 * A MAC is globally unique per chip already, which is exactly the property
 * needed here: flashing N boards with the identical firmware image now
 * gives N distinct device_ids for free, with no per-unit config to hand-edit
 * and no risk of two boards colliding because someone forgot to bump a
 * constant. */
static uint8_t s_device_id[ZC2X_DEVICE_ID_SIZE];
static uint32_t s_sequence = 0;
static twai_node_handle_t s_twai_node;
static QueueHandle_t s_can_rx_queue;
static QueueHandle_t s_nats_queue;
static QueueHandle_t s_xbee_tx_queue;

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

static const wifi_credential_t s_wifi_candidates[] = OBU_WIFI_CREDENTIALS;
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
    if (force_advance || s_wifi_fail_count >= OBU_WIFI_MAX_RETRIES_PER_CANDIDATE)
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
               OBU_WIFI_MAX_RETRIES_PER_CANDIDATE);
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
  assert(s_wifi_event_group != NULL);

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

static int nats_connect(const char *server)
{
  struct sockaddr_in addr = {
      .sin_family = AF_INET,
      .sin_port = htons(OBU_NATS_PORT),
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
   * s_nats_queue even once "connected". TCP_NODELAY sends each write
   * immediately instead. */
  int nodelay = 1;
  if (setsockopt(sock, IPPROTO_TCP, TCP_NODELAY, &nodelay, sizeof(nodelay)) != 0)
  {
    ESP_LOGW(TAG, "setsockopt(TCP_NODELAY) failed: errno %d", errno);
  }

  if (connect(sock, (struct sockaddr *)&addr, sizeof(addr)) != 0)
  {
    ESP_LOGE(TAG, "connect() to %s:%d failed: errno %d",
             server, OBU_NATS_PORT, errno);
    close(sock);
    return -1;
  }

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

  const char *connect_msg = "CONNECT {\"verbose\":false}\r\n";
  if (send(sock, connect_msg, strlen(connect_msg), 0) < 0)
  {
    ESP_LOGE(TAG, "send CONNECT failed");
    close(sock);
    return -1;
  }

  ESP_LOGI(TAG, "Connected to NATS %s:%d", server, OBU_NATS_PORT);
  return sock;
}

/* Publishes 1..OBU_NATS_BATCH_MAX_PACKETS packets as a single NATS message:
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
  uint8_t buf[64 + (OBU_NATS_BATCH_MAX_PACKETS * sizeof(zc2x_packet_t)) + 2];
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

static bool IRAM_ATTR on_can_rx_done(twai_node_handle_t node,
                                     const twai_rx_done_event_data_t *edata,
                                     void *user_ctx)
{
  uint8_t data_buf[8] = {0};
  twai_frame_t frame = {
      .buffer = data_buf,
      .buffer_len = sizeof(data_buf),
  };

  if (twai_node_receive_from_isr(node, &frame) != ESP_OK)
  {
    return false;
  }

  uint8_t dlc = (frame.header.dlc > 8U) ? 8U : (uint8_t)frame.header.dlc;

  can_rx_frame_t rx;
  rx.id = frame.header.id;
  rx.dlc = dlc;
  memcpy(rx.data, data_buf, dlc);

  BaseType_t higher_task_woken = pdFALSE;
  xQueueSendFromISR(s_can_rx_queue, &rx, &higher_task_woken);
  return higher_task_woken == pdTRUE;
}

/* -------------------------------------------------------------------------
 * TWAI bus diagnostics — logs error flags and error-state transitions
 * (ACTIVE/WARNING/PASSIVE/BUS_OFF). Purely observational: helps tell apart
 * "no signal reaching RX at all" (zero diag events, zero rx frames) from
 * "frames arriving but corrupted" (bit_err/stuff_err/form_err — usually
 * wiring/termination/ground) from "ACK errors driving bus-off" (ack_err —
 * see enable_self_test on the ECU simulator, and enable_listen_only in
 * can_init() below — OBU must actively ACK real bus traffic or any other
 * transmitter with no other node on the bus goes bus-off from missing ACK).
 * ---------------------------------------------------------------------- */
typedef struct
{
  bool is_error;
  twai_error_flags_t err_flags;
  twai_error_state_t old_sta;
  twai_error_state_t new_sta;
} can_diag_event_t;

static QueueHandle_t s_can_diag_queue;

static bool IRAM_ATTR on_can_error(twai_node_handle_t node,
                                   const twai_error_event_data_t *edata,
                                   void *user_ctx)
{
  can_diag_event_t evt = {.is_error = true, .err_flags = edata->err_flags};
  BaseType_t higher_task_woken = pdFALSE;
  xQueueSendFromISR(s_can_diag_queue, &evt, &higher_task_woken);
  return higher_task_woken == pdTRUE;
}

static bool IRAM_ATTR on_can_state_change(twai_node_handle_t node,
                                          const twai_state_change_event_data_t *edata,
                                          void *user_ctx)
{
  can_diag_event_t evt = {
      .is_error = false, .old_sta = edata->old_sta, .new_sta = edata->new_sta};
  BaseType_t higher_task_woken = pdFALSE;
  xQueueSendFromISR(s_can_diag_queue, &evt, &higher_task_woken);
  return higher_task_woken == pdTRUE;
}

static const char *can_error_state_name(twai_error_state_t s)
{
  switch (s)
  {
  case TWAI_ERROR_ACTIVE:
    return "ACTIVE";
  case TWAI_ERROR_WARNING:
    return "WARNING";
  case TWAI_ERROR_PASSIVE:
    return "PASSIVE";
  case TWAI_ERROR_BUS_OFF:
    return "BUS_OFF";
  default:
    return "UNKNOWN";
  }
}

static void can_diag_task(void *arg)
{
  can_diag_event_t evt;

  while (1)
  {
    if (xQueueReceive(s_can_diag_queue, &evt, portMAX_DELAY) != pdTRUE)
    {
      continue;
    }

    if (evt.is_error)
    {
      ESP_LOGW(TAG,
               "TWAI error: arb_lost=%u bit_err=%u form_err=%u stuff_err=%u ack_err=%u",
               (unsigned)evt.err_flags.arb_lost,
               (unsigned)evt.err_flags.bit_err,
               (unsigned)evt.err_flags.form_err,
               (unsigned)evt.err_flags.stuff_err,
               (unsigned)evt.err_flags.ack_err);
    }
    else
    {
      ESP_LOGW(TAG, "TWAI state change: %s -> %s",
               can_error_state_name(evt.old_sta),
               can_error_state_name(evt.new_sta));
    }
  }
}

static void xbee_init(void)
{
  const uart_config_t cfg = {
      .baud_rate = OBU_XBEE_BAUD,
      .data_bits = UART_DATA_8_BITS,
      .parity = UART_PARITY_DISABLE,
      .stop_bits = UART_STOP_BITS_1,
      .flow_ctrl = UART_HW_FLOWCTRL_DISABLE,
      .source_clk = UART_SCLK_DEFAULT,
  };

  ESP_ERROR_CHECK(uart_param_config(UART_NUM_1, &cfg));
  ESP_ERROR_CHECK(uart_set_pin(UART_NUM_1,
                               OBU_XBEE_TX_PIN, OBU_XBEE_RX_PIN,
                               UART_PIN_NO_CHANGE, UART_PIN_NO_CHANGE));
  ESP_ERROR_CHECK(uart_driver_install(UART_NUM_1, OBU_XBEE_UART_RX_BUF_SIZE,
                                      OBU_XBEE_UART_TX_BUF_SIZE, 0, NULL, 0));

  ESP_LOGI(TAG, "UART1 (XBee) ready: TX=%d RX=%d baud=%d",
           OBU_XBEE_TX_PIN, OBU_XBEE_RX_PIN, OBU_XBEE_BAUD);
}

static void can_init(void)
{
  s_can_rx_queue = xQueueCreate(OBU_CAN_RX_QUEUE_LEN, sizeof(can_rx_frame_t));
  assert(s_can_rx_queue != NULL);

  s_can_diag_queue = xQueueCreate(OBU_CAN_DIAG_QUEUE_LEN, sizeof(can_diag_event_t));
  assert(s_can_diag_queue != NULL);

  const twai_onchip_node_config_t node_cfg = {
      .io_cfg = {
          .tx = OBU_CAN_TX_PIN,
          .rx = OBU_CAN_RX_PIN,
          .quanta_clk_out = GPIO_NUM_NC,
          .bus_off_indicator = GPIO_NUM_NC,
      },
      .bit_timing = {
          .bitrate = OBU_CAN_BITRATE,
      },
      .tx_queue_depth = OBU_CAN_TX_QUEUE_DEPTH,
      /* Must NOT be listen-only: "No transmissions or acknowledgements"
       * (ESP-IDF's own doc for enable_listen_only) means OBU never drives
       * the ACK slot. If OBU is the only other node on the bus, every frame
       * the real bus master sends gets zero ACK — CAN auto-retransmits on
       * any transmission error with no delay, so one un-ACKed frame becomes
       * a back-to-back retry storm that saturates the bus with error frames
       * and drives the master's TEC into warning (status 0x60) and then
       * bus-off (0x80). Normal mode fixes this: OBU asserts ACK on valid
       * frames (automatic, protocol-level) but — with tx_queue_depth
       * effectively unused — still never transmits a CAN data frame of its
       * own, so it stays passive at the application level. Unlike ECU's
       * enable_self_test (which only lets ECU skip requiring ACK for its
       * *own* transmissions), this can't be worked around from the
       * transmitting side when that side is a real external node we don't
       * control the firmware of. */
      .flags = {
          .enable_listen_only = 0,
      },
  };
  ESP_ERROR_CHECK(twai_new_node_onchip(&node_cfg, &s_twai_node));

  const twai_mask_filter_config_t filter = {
      .id = 0,
      .mask = 0,
  };
  ESP_ERROR_CHECK(twai_node_config_mask_filter(s_twai_node, 0, &filter));

  const twai_event_callbacks_t cbs = {
      .on_rx_done = on_can_rx_done,
      .on_error = on_can_error,
      .on_state_change = on_can_state_change,
  };
  ESP_ERROR_CHECK(twai_node_register_event_callbacks(s_twai_node, &cbs, NULL));
  ESP_ERROR_CHECK(twai_node_enable(s_twai_node));

  ESP_LOGI(TAG, "TWAI (CAN) listening: TX=%d RX=%d %d bps",
           OBU_CAN_TX_PIN, OBU_CAN_RX_PIN, OBU_CAN_BITRATE);
}

#define XBEE_SYNC_0 ((uint8_t)0xAAu)
#define XBEE_SYNC_1 ((uint8_t)0x55u)
#define XBEE_SYNC_2 ((uint8_t)0xC2u)
#define XBEE_SYNC_3 ((uint8_t)0x58u)
#define XBEE_FRAME_SIZE (4U + sizeof(zc2x_packet_t)) /* sync + packet */

static void xbee_send_packet(const zc2x_packet_t *pkt)
{
  static const uint8_t sync[4] = {XBEE_SYNC_0, XBEE_SYNC_1,
                                  XBEE_SYNC_2, XBEE_SYNC_3};
  uart_write_bytes(UART_NUM_1, (const char *)sync, sizeof(sync));
  uart_write_bytes(UART_NUM_1, (const char *)pkt, sizeof(*pkt));
}

/* XBee UART TX has a hard physical ceiling (~303 pkt/sec at 115200 baud —
 * see OBU_XBEE_TX_QUEUE_LEN). Doing the blocking uart_write_bytes() call
 * directly inside can_rx_task meant a saturated XBee link stalled OBU's
 * highest-priority task on a single-core chip, which starved nats_task of
 * any CPU time — the WiFi/NATS path has nothing to do with XBee bandwidth
 * and shouldn't be affected by it. This task isolates that blocking I/O so
 * can_rx_task never performs any I/O of its own.
 *
 * uart_write_bytes() itself is NOT safe to call blindly under sustained
 * saturation: ESP-IDF's uart_tx_all() (esp_driver_uart/src/uart.c) loops
 * checking xRingbufferGetCurFreeSize() and only calls the (yielding)
 * xRingbufferSend() when free space is nonzero — when the ring buffer is
 * completely full, free stays 0, the yielding call is skipped entirely,
 * and the loop just spins re-checking free space with zero yield. That
 * busy-loop (confirmed by a task_wdt panic with
 * xbee_tx_task stuck inside xRingbufferGetCurFreeSize) is a real gap in
 * ESP-IDF's own driver, not something fixable from here — so we check
 * available space with the non-blocking uart_get_tx_buffer_free_size()
 * first and drop rather than ever risk calling uart_write_bytes() when
 * there isn't room, exactly like the non-blocking queue pattern used
 * everywhere else in this file. */
static void xbee_tx_task(void *arg)
{
  zc2x_packet_t pkt;
  uint32_t sent_since_log = 0;
  uint32_t wire_drops_since_log = 0;
  int64_t last_log_us = esp_timer_get_time();

  while (1)
  {
    int64_t now = esp_timer_get_time();
    if (now - last_log_us >= (int64_t)OBU_STATS_LOG_INTERVAL_MS * 1000)
    {
      ESP_LOGI(TAG, "stats: %lu XBee sent, %lu XBee wire-full drops in last %d ms",
               (unsigned long)sent_since_log, (unsigned long)wire_drops_since_log,
               OBU_STATS_LOG_INTERVAL_MS);
      sent_since_log = 0;
      wire_drops_since_log = 0;
      last_log_us = now;
    }

    if (xQueueReceive(s_xbee_tx_queue, &pkt, pdMS_TO_TICKS(OBU_STATS_LOG_INTERVAL_MS)) != pdTRUE)
    {
      continue;
    }

    size_t free_size = 0;
    if (uart_get_tx_buffer_free_size(UART_NUM_1, &free_size) != ESP_OK ||
        free_size < XBEE_FRAME_SIZE)
    {
      wire_drops_since_log++;
      continue;
    }

    xbee_send_packet(&pkt);
    sent_since_log++;
  }
}

static void can_rx_task(void *arg)
{
  can_rx_frame_t rx;
  zc2x_can_payload_t can_pl;
  zc2x_packet_t pkt;
  uint32_t frames_since_log = 0;
  uint32_t nats_drops_since_log = 0;
  uint32_t xbee_drops_since_log = 0;
  int64_t last_log_us = esp_timer_get_time();

  /* At real vehicle CAN bus rates (observed: thousands of frames/sec, well
   * above the ECU simulator's 10 Hz default) this task — highest priority,
   * single-core chip — can legitimately stay ready-to-run back-to-back for
   * seconds at a time, since xQueueReceive() only blocks when the RX queue
   * is briefly empty. That starves the IDLE task of any CPU time, and the
   * task watchdog by default only watches IDLE, so it fires a false-alarm
   * warning even though can_rx_task is behaving correctly, just busy (seen
   * in practice: task_wdt panic with can_rx_task mid-way through a routine
   * packet_crc16_compute() call — the CRC loop itself is bounded to a fixed
   * 32 bytes, not stuck). Subscribing this task to the watchdog directly and
   * feeding it on every loop iteration reports liveness accurately
   * regardless of how little idle time is available.
   *
   * The receive below therefore can't use portMAX_DELAY any more: if the
   * bus goes quiet for longer than the watchdog timeout (ignition off,
   * between runs — not a bug), a task blocked the whole time would never
   * reach the reset call either, trading one false alarm for another.
   * Bounding the wait keeps the loop — and the reset — cycling on a fixed
   * period regardless of whether traffic is bursty, saturated, or absent. */
  esp_task_wdt_add(NULL);

  while (1)
  {
    esp_task_wdt_reset();

    if (xQueueReceive(s_can_rx_queue, &rx, pdMS_TO_TICKS(1000)) != pdTRUE)
    {
      continue;
    }

    can_pl.id = rx.id;
    can_pl.dlc = rx.dlc;
    memcpy(can_pl.data, rx.data, rx.dlc);
    if (rx.dlc < 8U)
    {
      memset(can_pl.data + rx.dlc, 0, 8U - rx.dlc);
    }

    zc2x_result_t res = zc2x_packet_init(
        &pkt,
        ZC2X_PACKET_TYPE_CAN_FRAME,
        s_device_id,
        s_sequence,
        (uint64_t)esp_timer_get_time(),
        can_pl.id,
        can_pl.data,
        can_pl.dlc);

    if (res != ZC2X_OK)
    {
      ESP_LOGE(TAG, "packet_init failed (%d) seq=%lu dropped",
               res, (unsigned long)s_sequence);
      s_sequence++;
      continue;
    }

    /* Both sends are non-blocking: can_rx_task must never perform I/O of
     * its own (see xbee_tx_task above) or the XBee link's ~303 pkt/sec
     * ceiling and the NATS/WiFi path can each stall the other again. */
    if (xQueueSend(s_xbee_tx_queue, &pkt, 0) != pdTRUE)
    {
      xbee_drops_since_log++;
    }

    if (xQueueSend(s_nats_queue, &pkt, 0) != pdTRUE)
    {
      nats_drops_since_log++;
    }

    frames_since_log++;
    s_sequence++;

    /* Periodic summary instead of a log line per frame — see
     * OBU_STATS_LOG_INTERVAL_MS in device_config.h for why. */
    int64_t now = esp_timer_get_time();
    if (now - last_log_us >= (int64_t)OBU_STATS_LOG_INTERVAL_MS * 1000)
    {
      ESP_LOGI(TAG, "stats: %lu CAN frames, %lu NATS-queue drops, %lu XBee-queue "
                    "drops in last %d ms (seq=%lu)",
               (unsigned long)frames_since_log, (unsigned long)nats_drops_since_log,
               (unsigned long)xbee_drops_since_log,
               OBU_STATS_LOG_INTERVAL_MS, (unsigned long)s_sequence);
      frames_since_log = 0;
      nats_drops_since_log = 0;
      xbee_drops_since_log = 0;
      last_log_us = now;
    }
  }
}

static void nats_task(void *arg)
{
  int nats_sock = -1;
  zc2x_packet_t batch[OBU_NATS_BATCH_MAX_PACKETS];
  size_t batch_count = 0;
  uint32_t published_since_log = 0;
  uint32_t batches_since_log = 0;
  int64_t last_log_us = esp_timer_get_time();

  while (1)
  {
    /* Periodic summary instead of a log line per publish — see
     * OBU_STATS_LOG_INTERVAL_MS in device_config.h for why. Checked at the
     * top of the loop so it still fires during WiFi/NATS outages. wifi/sock
     * state is included directly so a stuck "0 publishes" period doesn't
     * require correlating against wifi_event_handler's own log lines to
     * tell apart "no WiFi" from "WiFi up, NATS itself not connecting". */
    int64_t now = esp_timer_get_time();
    if (now - last_log_us >= (int64_t)OBU_STATS_LOG_INTERVAL_MS * 1000)
    {
      bool wifi_up = (xEventGroupGetBits(s_wifi_event_group) & WIFI_CONNECTED_BIT) != 0;
      ESP_LOGI(TAG, "stats: %lu NATS publishes in %lu batches in last %d ms "
                    "(wifi=%s candidate=%s sock=%s)",
               (unsigned long)published_since_log, (unsigned long)batches_since_log,
               OBU_STATS_LOG_INTERVAL_MS, wifi_up ? "up" : "down",
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
       * OBU_WIFI_MAX_RETRIES_PER_CANDIDATE's disconnect-counting logic can
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
              (int64_t)OBU_WIFI_DHCP_TIMEOUT_MS * 1000)
      {
        ESP_LOGW(TAG, "WiFi candidate %s: no IP after %d ms (associated but stuck? "
                      "captive portal / DHCP issue) — forcing disconnect+advance",
                 s_wifi_candidates[s_wifi_candidate_idx].ssid, OBU_WIFI_DHCP_TIMEOUT_MS);
        s_wifi_force_advance = true;
        esp_wifi_disconnect();
      }

      vTaskDelay(pdMS_TO_TICKS(OBU_WIFI_WAIT_DELAY_MS));
      continue;
    }

    if (nats_sock < 0)
    {
      const char *server = s_wifi_candidates[s_wifi_candidate_idx].nats_server;
      nats_sock = nats_connect(server);
      if (nats_sock < 0)
      {
        ESP_LOGW(TAG, "NATS connect to %s failed — retrying in %d ms",
                 server, OBU_NATS_RECONNECT_DELAY_MS);
        vTaskDelay(pdMS_TO_TICKS(OBU_NATS_RECONNECT_DELAY_MS));
        continue;
      }
    }

    /* Accumulate into batch[]; flush on whichever comes first: batch full,
     * or no new packet within OBU_NATS_BATCH_MAX_AGE_MS (bounds worst-case
     * publish latency during low/sporadic traffic). This also drains
     * s_nats_queue faster during bursts, since the slow network I/O now
     * happens once per batch instead of once per packet. */
    bool got_packet = xQueueReceive(s_nats_queue, &batch[batch_count],
                                    pdMS_TO_TICKS(OBU_NATS_BATCH_MAX_AGE_MS)) == pdTRUE;
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

    if (got_packet && batch_count < OBU_NATS_BATCH_MAX_PACKETS)
    {
      continue; /* keep accumulating */
    }

    if (nats_publish(nats_sock, OBU_NATS_SUBJECT, batch, batch_count) != 0)
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

void app_main(void)
{
  ESP_LOGI(TAG, "ZC2X OBU starting");

  /* esp_read_mac needs no WiFi/BT driver initialized -- it just derives the
   * requested interface's MAC from the factory-burned base MAC in eFuse --
   * so this is safe to call before wifi_init() below. ESP_MAC_WIFI_STA
   * specifically (not ESP_MAC_BASE) so device_id matches the MAC this same
   * board actually presents on the network it publishes over -- traceable
   * in a DHCP client list or packet capture, not just an opaque identifier. */
  ESP_ERROR_CHECK(esp_read_mac(s_device_id, ESP_MAC_WIFI_STA));
  ESP_LOGI(TAG, "device_id (WiFi STA MAC): %02x%02x%02x%02x%02x%02x",
           s_device_id[0], s_device_id[1], s_device_id[2],
           s_device_id[3], s_device_id[4], s_device_id[5]);

  esp_err_t ret = nvs_flash_init();
  if (ret == ESP_ERR_NVS_NO_FREE_PAGES ||
      ret == ESP_ERR_NVS_NEW_VERSION_FOUND)
  {
    ESP_ERROR_CHECK(nvs_flash_erase());
    ret = nvs_flash_init();
  }
  ESP_ERROR_CHECK(ret);

  s_nats_queue = xQueueCreate(OBU_NATS_QUEUE_LEN, sizeof(zc2x_packet_t));
  assert(s_nats_queue != NULL);

  s_xbee_tx_queue = xQueueCreate(OBU_XBEE_TX_QUEUE_LEN, sizeof(zc2x_packet_t));
  assert(s_xbee_tx_queue != NULL);

  xbee_init();
  can_init();
  wifi_init();

  xTaskCreate(can_rx_task, "can_rx_task", OBU_CAN_RX_TASK_STACK, NULL,
              configMAX_PRIORITIES - 1, NULL);
  xTaskCreate(can_diag_task, "can_diag_task", OBU_CAN_DIAG_TASK_STACK, NULL,
              configMAX_PRIORITIES - 3, NULL);
  xTaskCreate(nats_task, "nats_task", OBU_NATS_TASK_STACK, NULL,
              configMAX_PRIORITIES - 2, NULL);
  /* Below nats_task, not equal to it: unlike RSU's uart_rx_task (which is
   * idle/blocked most of the time — XBee RX only ever delivers what OBU
   * managed to transmit, inherently <= the link's own capacity),
   * xbee_tx_task is feeding a link already confirmed to run at/above its
   * ~303 pkt/sec physical ceiling — it is continuously busy, not
   * intermittent. Giving it nats_task's own priority tier lets a
   * perpetually-saturated, wire-limited task keep eating nats_task's CPU
   * share for no benefit (more CPU priority can't create wire bandwidth
   * that doesn't exist), which is exactly the asymmetry that made OBU's
   * NATS path lag behind RSU's even after decoupling the two queues. */
  xTaskCreate(xbee_tx_task, "xbee_tx_task", OBU_XBEE_TX_TASK_STACK, NULL,
              configMAX_PRIORITIES - 3, NULL);
}
