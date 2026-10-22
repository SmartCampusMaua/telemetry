/**
 * @file main.c
 * @brief ECU simulator firmware entry point.
 *
 * Data path: ECU simulator -> CAN (TWAI) -> OBU -> XBee -> RSU -> NATS
 *
 * Simulates the CAN2 message/signal set defined in
 * docs/architecture/zc2x-can2.dbc (the layout MoTeC M1 Tune transmits on
 * CAN2 in production) so OBU/RSU/services/input/nats can be validated
 * end-to-end without the car. Each message is emitted on its own configured
 * period (device_config.h) with per-signal values that wander plausibly
 * within realistic physical ranges via a bounded random walk, rather than
 * static bytes.
 */

#include <assert.h>
#include <stdint.h>
#include <string.h>

#include "FreeRTOSConfig.h"
#include "device_config.h"
#include "esp_log.h"
#include "esp_now.h"
#include "esp_random.h"
#include "esp_timer.h"
#include "esp_twai.h"
#include "esp_twai_onchip.h"
#include "esp_twai_types.h"
#include "esp_wifi.h"
#include "esp_wifi_types_generic.h"
#include "freertos/FreeRTOS.h"
#include "freertos/idf_additions.h"
#include "freertos/projdefs.h"
#include "freertos/task.h"
#include "nvs_flash.h"
#include "portmacro.h"

static const char *TAG = "ECU";
static twai_node_handle_t s_twai_node;
static QueueHandle_t s_esp_now_queue;

typedef struct {
  uint8_t id;
  float extern_sensor;
  float middle_sensor;
  float internal_sensor;
} data_packet_t;




static void can_init(void) {
  const twai_onchip_node_config_t node_cfg = {
      .io_cfg =
          {
              .tx = 1,
              .rx = 21,
              // .tx = ECU_CAN_TX_PIN,
              // .rx = ECU_CAN_RX_PIN,
              .quanta_clk_out = GPIO_NUM_NC,
              .bus_off_indicator = GPIO_NUM_NC,
          },
      .bit_timing =
          {
              .bitrate = ECU_CAN_BITRATE,
          },
      .tx_queue_depth = ECU_CAN_TX_QUEUE_DEPTH,
      .flags =
          {
              /* ECU is a bench-test simulator: on a bus with no other node,
               * ECU's own frames would get an ACK error on every transmission
               * and drive it into bus-off after ~30 frames. enable_self_test
               * makes ECU satisfy its own ACK requirement, which is exactly
               * what it's for ("Use this mode for self testing" —
               * esp_twai_onchip.h). Kept on regardless of OBU's own CAN mode
               * (OBU runs Normal/ACKing today — see obu/main/main.c can_init())
               * so this simulator stays robust even bench-tested alone. Real
               * vehicle ECUs (e.g. the MoTeC M180 this simulates) don't need
               * this since a live bus always has other nodes to ACK. */
              .enable_self_test = 1,
              .enable_listen_only = 0,
          },
  };

  ESP_ERROR_CHECK(twai_new_node_onchip(&node_cfg, &s_twai_node));
  ESP_ERROR_CHECK(twai_node_enable(s_twai_node));

  ESP_LOGI(TAG, "TWAI (CAN) tx ready: TX=%d RX=%d bitrate=%d", ECU_CAN_TX_PIN,
           ECU_CAN_RX_PIN, ECU_CAN_BITRATE);
}

static void can_tx_task(void *arg) {
  uint32_t tick = 0;
  uint32_t sent_since_log = 0;
  uint32_t failed_since_log = 0;
  int64_t last_log_us = esp_timer_get_time();
  data_packet_t data_packet;

  while (1) {
    if (xQueueReceive(s_esp_now_queue, &data_packet, pdMS_TO_TICKS(100)) != pdTRUE) {
      continue;
    }
    /* Persistent per-message storage — see the comment on
     * s_payload_buffers/s_frames above for why this can't be stack-local. */
    uint8_t id = data_packet.id;
    int16_t extern_sensor_int   = (int16_t)((data_packet.extern_sensor   * 100));
    int16_t middle_sensor_int   = (int16_t)((data_packet.middle_sensor   * 100));
    int16_t internal_sensor_int = (int16_t)((data_packet.internal_sensor * 100));
    uint8_t payload[6] = {
      (extern_sensor_int >> 8) & 0xFF,
      extern_sensor_int & 0xFF,
      (middle_sensor_int >> 8) & 0xFF,
      middle_sensor_int & 0xFF,
      (internal_sensor_int >> 8) & 0xFF,
      internal_sensor_int & 0xFF
    };
    uint32_t can_id = 304U;
    if (id != 0) {
      can_id = 336U;
    }
    twai_frame_t message_frame = (twai_frame_t){
        .header =
            {
                .id = can_id,
                .ide = 0,
                .rtr = 0,
                .dlc = sizeof(payload),
            },
        .buffer = payload,
        .buffer_len = sizeof(payload),
    };
    
    ESP_LOGI(TAG, "values: e: %d, m: %d, i: %d", extern_sensor_int, middle_sensor_int, internal_sensor_int);

    esp_err_t err =
        twai_node_transmit(s_twai_node, &message_frame, pdMS_TO_TICKS(100));
    if (err != ESP_OK) {
      failed_since_log++;
      ESP_LOGW(TAG, "CAN tx 0x%03lx failed (err=0x%x)",
               (unsigned long)id, (unsigned)err);    } else {
      sent_since_log++;
    }
  }

  /* Periodic summary instead of per-frame logging (which would flood the
   * console at up to 300 msgs/sec — see OBU_STATS_LOG_INTERVAL_MS for the
   * same reasoning elsewhere in this repo). Also closes a real
   * debuggability gap: with no periodic log at all, ECU running correctly
   * and ECU stuck/silent looked identical from the outside — exactly the
   * ambiguity that made an earlier real bug here harder to pin down. */
  int64_t now = esp_timer_get_time();
  if (now - last_log_us >= (int64_t)ECU_STATS_LOG_INTERVAL_MS * 1000) {
    ESP_LOGI(TAG, "stats: %lu CAN tx queued, %lu tx failures in last %d ms",
             (unsigned long)sent_since_log, (unsigned long)failed_since_log,
             ECU_STATS_LOG_INTERVAL_MS);
    sent_since_log = 0;
    failed_since_log = 0;
    last_log_us = now;
  }

  tick++;
  vTaskDelay(pdMS_TO_TICKS(ECU_CAN_TICK_MS));
}

static void esp_now_recv_cb(const esp_now_recv_info_t *recv_info,
                            const uint8_t *data, int len) {

  if (len != sizeof(data_packet_t)) {
    ESP_LOGW(TAG, "Unexpected data length received: %d bytes", len);
    return;
  }

  data_packet_t received_data;

  memcpy(&received_data, data, sizeof(received_data));
  ESP_LOGI(TAG, "Packet ID: %d", received_data.id);
  ESP_LOGI(TAG, "Message: e:%f m:%f i:%f", received_data.extern_sensor, received_data.middle_sensor, received_data.internal_sensor);
  BaseType_t higher_task_woken = pdFALSE;
  xQueueSendFromISR(s_esp_now_queue, &received_data, &higher_task_woken);
}

static void wifi_init(void) {
  ESP_ERROR_CHECK(esp_netif_init());
  ESP_ERROR_CHECK(esp_event_loop_create_default());

  wifi_init_config_t cfg = WIFI_INIT_CONFIG_DEFAULT();
  ESP_ERROR_CHECK(esp_wifi_init(&cfg));

  ESP_ERROR_CHECK(esp_wifi_set_storage(WIFI_STORAGE_RAM));
  ESP_ERROR_CHECK(esp_wifi_set_mode(WIFI_MODE_STA));
  ESP_ERROR_CHECK(esp_wifi_start());
}

static void esp_now_receiver_init(void) {
  esp_err_t ret = nvs_flash_init();
  if (ret == ESP_ERR_NVS_NO_FREE_PAGES ||
      ret == ESP_ERR_NVS_NEW_VERSION_FOUND) {
    ESP_ERROR_CHECK(nvs_flash_erase());
    ret = nvs_flash_init();
  }
  ESP_ERROR_CHECK(ret);

  wifi_init();

  ESP_ERROR_CHECK(esp_now_init());

  ESP_ERROR_CHECK(esp_now_register_recv_cb(esp_now_recv_cb));

  ESP_LOGI(TAG,
           "ESP-NOW Receiver initialized successfully. Waiting for data...");
  s_esp_now_queue = xQueueCreate(1024, sizeof(data_packet_t));
  assert(s_esp_now_queue != NULL);
}

void app_main(void) {
  ESP_LOGI(TAG, "ZC2X TTU simulator starting");

  esp_now_receiver_init();
  can_init();

  xTaskCreate(can_tx_task, "can_tx_task", ECU_CAN_TX_TASK_STACK, NULL,
              configMAX_PRIORITIES - 1, NULL);
}
