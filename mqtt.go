package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type aquareaMQTT struct {
	mqttClient     mqtt.Client
	commandChannel chan aquareaCommand
	online         atomic.Bool // last known availability, re-announced on (re)connect
}

func mqttHandler(ctx context.Context, wg *sync.WaitGroup, config configType, dataChannel chan map[string]string, commandChannel chan aquareaCommand, statusChannel chan bool) {
	defer wg.Done()
	log.Println("Starting MQTT handler")
	mqttKeepalive := 30 * time.Second // MQTT spec default
	if config.MqttKeepalive != "" {
		var err error
		mqttKeepalive, err = time.ParseDuration(config.MqttKeepalive)
		if err != nil {
			log.Fatal(err)
		}
	}

	var mqttInstance aquareaMQTT
	mqttInstance.commandChannel = commandChannel
	mqttInstance.makeMQTTConn(config.MqttServer, config.MqttPort, config.MqttLogin, config.MqttPass, config.MqttClientID, mqttKeepalive)
	defer mqttInstance.mqttClient.Disconnect(2000)
	defer mqttInstance.setStatus(false)

	for {
		select {
		case dataToPublish := <-dataChannel:
			mqttInstance.publish(dataToPublish)
		case online := <-statusChannel:
			mqttInstance.setStatus(online)
		case <-ctx.Done():
			return
		}
	}
}

func (am *aquareaMQTT) makeMQTTConn(mqttServer string, mqttPort int, mqttLogin, mqttPass, mqttClientID string, mqttKeepalive time.Duration) {
	log.Println("Connecting to MQTT broker")
	//set MQTT options
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("%s://%s:%v", "tcp", mqttServer, mqttPort))
	opts.SetPassword(mqttPass)
	opts.SetUsername(mqttLogin)
	opts.SetClientID(mqttClientID)
	opts.SetKeepAlive(mqttKeepalive)

	opts.SetCleanSession(true)  // don't want to receive entire backlog of setting changes
	opts.SetAutoReconnect(true) // default, but I want it explicit (exponential backoff on reconnect)
	opts.SetConnectRetry(true)  // keep retrying the initial connection while the broker is down
	opts.SetConnectRetryInterval(30 * time.Second)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Println("MQTT connected")
		c.Subscribe("aquarea/+/settings/+/set", 2, am.handleSubscription)
		// Re-announce availability: the Aquarea login can complete before the
		// first broker connection is up, and with CleanSession paho silently
		// drops publishes made before CONNACK (it resets the outbound store).
		am.publishStatus()
	})

	opts.SetWill("aquarea/status", "offline", byte(0), true)

	// connect to broker; with ConnectRetry the client keeps trying in the
	// background until the broker is up (publishes made meanwhile are queued),
	// so the handler must not block on the token here
	am.mqttClient = mqtt.NewClient(opts)
	am.mqttClient.Connect()

	am.setStatus(false) // offline till Service Cloud is connected
}

func (am *aquareaMQTT) setStatus(online bool) {
	am.online.Store(online)
	am.publishStatus()
}

func (am *aquareaMQTT) publishStatus() {
	status := "offline"
	if am.online.Load() {
		status = "online"
	}
	am.mqttClient.Publish("aquarea/status", byte(0), true, status)
}

func (am *aquareaMQTT) handleSubscription(mclient mqtt.Client, msg mqtt.Message) {
	topicPieces := strings.Split(msg.Topic(), "/")
	if len(topicPieces) > 3 {
		deviceID := topicPieces[1]
		setting := topicPieces[3]

		log.Printf("Received: Device ID %s setting: %s", deviceID, setting)
		am.commandChannel <- aquareaCommand{deviceID, setting, string(msg.Payload())}
	}
}

func (am *aquareaMQTT) publish(data map[string]string) {
	for key, value := range data {
		token := am.mqttClient.Publish(key, byte(0), true, value)
		// With CleanSession, messages published before CONNACK are dropped from
		// the store on connect and their token never completes - waiting on it
		// would hang the handler forever. The next poll re-sends the data.
		if am.mqttClient.IsConnectionOpen() {
			if token.Wait() && token.Error() != nil {
				log.Printf("Fail to publish to %s: %v", key, token.Error())
			}
		}
	}
}
