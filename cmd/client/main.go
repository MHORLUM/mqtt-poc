package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

type ClientStatus struct {
	ClientID  string `json:"client_id"`
	Timestamp string `json:"timestamp"`
}

func main() {
	clientID := uuid.New().String()[:8] // สร้าง ID แบบสุ่ม
	fmt.Printf("Starting client [%s]\n", clientID)

	opts := MQTT.NewClientOptions().AddBroker("tcp://broker.emqx.io:1883")
	opts.SetClientID(clientID)

	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	// ส่งข้อมูลทุกๆ สุ่มระหว่าง 3-7 วินาที
	ticker := time.NewTicker(time.Duration(3+rand.Intn(5)) * time.Second)
	defer ticker.Stop()

	// จัดการการหยุดทำงาน
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-ticker.C:
			sendUpdate(client, clientID)
		case <-sigChan:
			fmt.Println("\nShutting down...")
			return
		}
	}
}

func sendUpdate(client MQTT.Client, clientID string) {
	status := ClientStatus{
		ClientID:  clientID,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	payload, _ := json.Marshal(status)
	if token := client.Publish("clients/status", 0, false, payload); token.Wait() && token.Error() != nil {
		fmt.Printf("Publish error: %v\n", token.Error())
	}
}