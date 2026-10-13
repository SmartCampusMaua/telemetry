package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SmartCampusMaua/telemetry/packages/go-config"
	"github.com/SmartCampusMaua/telemetry/services/bridge/chirpstackv4/internal"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

func connLostHandler(c MQTT.Client, err error) {
	fmt.Printf("Connection lost, reason: %v\n", err)
	os.Exit(1)
}

func main() {
	INFRA_CONFIG_FILE := os.Getenv("INFRA_CONFIG_FILE")
	if INFRA_CONFIG_FILE == "" {
		INFRA_CONFIG_FILE = "infra.json"
	}
	infraCfg, err := config.LoadInfraConfig(INFRA_CONFIG_FILE)
	if err != nil {
		panic(fmt.Sprintf("failed to load infra config (%s): %v", INFRA_CONFIG_FILE, err))
	}

	id := uuid.New().String()
	var sbMqttSubClientId strings.Builder
	var sbMqttPubClientId strings.Builder
	var sbPubTopic strings.Builder
	sbMqttSubClientId.WriteString("mqtt-topic-rewrite-lns-chirpstackv4-")
	sbMqttSubClientId.WriteString(id)
	sbMqttPubClientId.WriteString("mqtt-topic-rewrite-lns-chirpstackv4-")
	sbMqttPubClientId.WriteString(id)

	mqttSubBroker := "mqtt://networkserver2.maua.br:1883"
	mqttSubClientId := sbMqttSubClientId.String()
	mqttSubUser := ""
	mqttSubPassword := ""
	mqttSubQos := 0

	mqttSubOpts := MQTT.NewClientOptions()
	mqttSubOpts.AddBroker(mqttSubBroker)
	mqttSubOpts.SetClientID(mqttSubClientId)
	mqttSubOpts.SetUsername(mqttSubUser)
	mqttSubOpts.SetPassword(mqttSubPassword)
	mqttSubOpts.SetConnectionLostHandler(connLostHandler)

	mqttSubTopics := map[string]byte{
		// DEVICES REGISTRED VIA THE REGISTRY API (see registry.go)
		// "application/deb35cab-8a9a-42a9-b19e-0cd2ac859cc8/device/+/event/up": byte(mqttSubQos), // DET
		// "application/cf909d7a-a970-4473-9ef3-2c0618e1eb63/device/+/event/up": byte(mqttSubQos), // Emma
		// "application/33d3fb39-c249-4f8d-b105-2706af00bf5c/device/+/event/up": byte(mqttSubQos), // EnergyMeter
		// "application/3522bea0-3ecc-43dd-a676-824d2efc9a5a/device/+/event/up": byte(mqttSubQos), // GaugePressure
		// "application/c31059f6-2a9a-49c5-80e5-c912829f433a/device/+/event/up": byte(mqttSubQos), // Hydrometer
		// "application/815dcf59-4f43-45c7-9b0a-e264db48232d/device/+/event/up": byte(mqttSubQos), // MauaSat
		// "application/4ae0c733-e9b5-482f-8542-3d08f8e6d077/device/+/event/up": byte(mqttSubQos), // SmartLight
		// "application/25e85005-adc9-48d6-89e1-b4f677cf18ef/device/+/event/up": byte(mqttSubQos), // WaterTankLevel
		"application/a7d603f2-3de4-4516-82f5-3323a3a80467/device/+/event/up": byte(mqttSubQos), // NIT21LI_EMW104
		"application/e2cbf2fb-fb26-4608-aacc-66115c0521c0/device/+/event/up": byte(mqttSubQos), // imt_smdl - SoilMoisture3DepthLevels
		"application/5239fc35-6b28-4908-89fa-4efa9bf0636e/device/+/event/up": byte(mqttSubQos), // Sprinkler imt_svc - solenoid valve control
		// "application/083c08e7-aa02-41a7-82cb-923ba7c11f53/device/+/event/up": byte(mqttSubQos), // MilkFat
		// "application/15edcb63-f291-4333-ac61-13eb743ad8ef/device/+/event/up": byte(mqttSubQos), // GPS
		// "application/8e20d344-cd01-4503-8ffc-36735af8b2c1/device/+/event/up": byte(mqttSubQos), // VibrationAverage
		// "application/72cf3110-59c3-4d95-9107-015c1573e38a/device/+/event/up": byte(mqttSubQos), // 8PointTemperature
		"application/8bcb6d0a-9ab8-4699-ab66-8bee202367a7/device/+/event/up": byte(mqttSubQos), // SmartCampusMaua
	}

	mqttPubBroker := infraCfg.MQTT.Broker
	mqttPubClientId := sbMqttPubClientId.String()
	mqttPubUser := infraCfg.MQTT.Username
	mqttPubPassword := infraCfg.MQTT.Password
	mqttPubQos := 0

	mqttPubOpts := MQTT.NewClientOptions()
	mqttPubOpts.AddBroker(mqttPubBroker)
	mqttPubOpts.SetClientID(mqttPubClientId)
	mqttPubOpts.SetUsername(mqttPubUser)
	mqttPubOpts.SetPassword(mqttPubPassword)

	c := make(chan [2]string)

	mqttSubOpts.SetDefaultPublishHandler(func(mqttSubClient MQTT.Client, msg MQTT.Message) {
		c <- [2]string{msg.Topic(), string(msg.Payload())}
	})

	mqttSubClient := MQTT.NewClient(mqttSubOpts)
	if token := mqttSubClient.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	} else {
		fmt.Printf("Connected to %s\n", mqttSubBroker)
	}

	pClient := MQTT.NewClient(mqttPubOpts)
	if token := pClient.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	} else {
		fmt.Printf("Connected to %s\n", mqttPubBroker)
	}

	if token := mqttSubClient.SubscribeMultiple(mqttSubTopics, nil); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}

	// REGISTRY_API_URL points at the SmartCampusMaua `api` repo's registry
	// endpoint — the allowed dev_eui list is loaded from there now instead
	// of a local devices.json. See registry.go.
	REGISTRY_API_URL := infraCfg.Registry.APIURL
	DEVICE_REGISTRY_REFRESH_SEC := 30
	if raw := os.Getenv("DEVICE_REGISTRY_REFRESH_SEC"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			DEVICE_REGISTRY_REFRESH_SEC = v
		}
	}

	allowedDevEUIs, err := chirpstackv4.GetAllowedDevEUIs(REGISTRY_API_URL)
	if err != nil {
		panic(fmt.Sprintf("failed to load device registry (%s): %v", REGISTRY_API_URL, err))
	}
	fmt.Printf("Device registry loaded: api=%s devEUIs=%d\n", REGISTRY_API_URL, len(allowedDevEUIs))

	var devicesMu sync.RWMutex
	refreshTicker := time.NewTicker(time.Duration(DEVICE_REGISTRY_REFRESH_SEC) * time.Second)
	defer refreshTicker.Stop()
	go func() {
		for range refreshTicker.C {
			nextAllowedDevEUIs, err := chirpstackv4.GetAllowedDevEUIs(REGISTRY_API_URL)
			if err != nil {
				fmt.Printf("Device registry reload failed: %v\n", err)
				continue
			}
			devicesMu.Lock()
			allowedDevEUIs = nextAllowedDevEUIs
			devicesMu.Unlock()
			fmt.Printf("Device registry reloaded: api=%s devEUIs=%d\n", REGISTRY_API_URL, len(nextAllowedDevEUIs))
		}
	}()
	isAllowedDevEUI := func(devEUI string) bool {
		devicesMu.RLock()
		defer devicesMu.RUnlock()
		return allowedDevEUIs[strings.ToLower(devEUI)]
	}

	for {
		incoming := <-c
		s := strings.Split(incoming[0], "/")
		// application/APPLICATION_ID/device/DEVICE_ID/command/down
		var forward bool

		switch s[1] {
		// case "deb35cab-8a9a-42a9-b19e-0cd2ac859cc8":
		// 	measurement = "DET"

		// case "cf909d7a-a970-4473-9ef3-2c0618e1eb63":
		// 	measurement = "Emma"

		// case "33d3fb39-c249-4f8d-b105-2706af00bf5c":
		// 	measurement = "EnergyMeter"

		// case "3522bea0-3ecc-43dd-a676-824d2efc9a5a":
		// 	measurement = "GaugePressure"

		// case "c31059f6-2a9a-49c5-80e5-c912829f433a":
		// 	measurement = "Hydrometer"

		// case "815dcf59-4f43-45c7-9b0a-e264db48232d":
		// 	measurement = "MauaSat"

		// case "4ae0c733-e9b5-482f-8542-3d08f8e6d077":
		// 	measurement = "SmartLight"

		// case "25e85005-adc9-48d6-89e1-b4f677cf18ef":
		// 	measurement = "WaterTankLevel"

		case "a7d603f2-3de4-4516-82f5-3323a3a80467":
			forward = isAllowedDevEUI(s[3]) // NIT21LI_EMW104

		case "e2cbf2fb-fb26-4608-aacc-66115c0521c0":
			forward = isAllowedDevEUI(s[3]) // imt_smdl - SoilMoisture3DepthLevels

		case "5239fc35-6b28-4908-89fa-4efa9bf0636e":
			forward = isAllowedDevEUI(s[3]) // imt_svc - solenoid valve control

		// case "083c08e7-aa02-41a7-82cb-923ba7c11f53":
		// 	measurement = "MilkFat"

		// case "15edcb63-f291-4333-ac61-13eb743ad8ef":
		// 	measurement = "GPS"

		// case "8e20d344-cd01-4503-8ffc-36735af8b2c1":
		// 	measurement = "VibrationAverage"

		// case "72cf3110-59c3-4d95-9107-015c1573e38a":
		// 	measurement = "Temperature8Point"

		case "8bcb6d0a-9ab8-4699-ab66-8bee202367a7":
			forward = isAllowedDevEUI(s[3]) // SmartCampusMaua
		}

		if forward {
			sbPubTopic.Reset()
			sbPubTopic.WriteString("device/")
			sbPubTopic.WriteString(s[3])
			sbPubTopic.WriteString("/telemetry")
			token := pClient.Publish(sbPubTopic.String(), byte(mqttPubQos), false, incoming[1])
			token.Wait()
		}

	}
}
