package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

type ClientStatus struct {
	ClientID  string `json:"client_id"`
	Timestamp string `json:"timestamp"`
}

var (
	tmpChunks   = make(map[int][]byte)
	totalChunks int
	fileName    string
	mqttClient  MQTT.Client
	clientID    string
)

func main() {
	clientID = uuid.New().String()[:8]
	fmt.Printf("Starting client [%s]\n", clientID)

	opts := MQTT.NewClientOptions().AddBroker("tcp://localhost:1883")
	opts.SetClientID(clientID)
	opts.SetWill("clients/disconnected", clientID, 0, false)

	mqttClient = MQTT.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	mqttClient.Subscribe(fmt.Sprintf("file/send/%s/start", clientID), 1, onFileStart)
	mqttClient.Subscribe(fmt.Sprintf("file/send/%s/chunk", clientID), 1, onFileChunk)
	mqttClient.Subscribe(fmt.Sprintf("file/send/%s/end", clientID), 1, onFileEnd)

	sendUpdate(mqttClient, clientID)

	ticker := time.NewTicker(time.Duration(3+rand.Intn(5)) * time.Second)
	defer ticker.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-ticker.C:
			sendUpdate(mqttClient, clientID)
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

func onFileStart(client MQTT.Client, msg MQTT.Message) {
	tmpChunks = make(map[int][]byte)
	var meta struct {
		Filename    string `json:"filename"`
		TotalChunks int    `json:"total_chunks"`
	}
	if err := json.Unmarshal(msg.Payload(), &meta); err != nil {
		fmt.Printf("Error parsing file start: %v\n", err)
		return
	}
	fileName = meta.Filename
	totalChunks = meta.TotalChunks
	fmt.Printf("Receiving file: %s (%d chunks)\n", fileName, totalChunks)
}

func onFileChunk(client MQTT.Client, msg MQTT.Message) {
	payload := msg.Payload()
	parts := strings.SplitN(string(payload), "/", 2)
	if len(parts) < 2 {
		fmt.Println("Invalid chunk format")
		return
	}
	index, err := strconv.Atoi(parts[0])
	if err != nil {
		fmt.Println("Invalid chunk index")
		return
	}
	tmpChunks[index] = []byte(parts[1])
}

func onFileEnd(client MQTT.Client, msg MQTT.Message) {
	filePath := filepath.Join("received_" + fileName)
	f, err := os.Create(filePath)
	if err != nil {
		fmt.Printf("Error creating file: %v\n", err)
		return
	}
	defer f.Close()

	hash := sha256.New()
	for i := 0; i < totalChunks; i++ {
		if chunk, ok := tmpChunks[i]; ok {
			f.Write(chunk)
			hash.Write(chunk)
		} else {
			fmt.Printf("Missing chunk: %d\n", i)
			return
		}
	}

	sum := hex.EncodeToString(hash.Sum(nil))
	fmt.Printf("File received and saved as: %s\n", filePath)
	fmt.Printf("SHA256: %s\n", sum)

	ack := map[string]string{
		"client_id": clientID,
		"filename":  fileName,
		"sha256":    sum,
	}
	ackPayload, _ := json.Marshal(ack)
	mqttClient.Publish("clients/ack", 0, false, ackPayload)
}
