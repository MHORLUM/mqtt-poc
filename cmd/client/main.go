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
	Count     int    `json:"count"`
}

var (
	tmpChunks    = make(map[int][]byte)
	totalChunks  int
	fileName     string
	expectedHash string
	mqttClient   MQTT.Client
	clientID     string
	count        int // เพิ่มตัวแปร count
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
	// Subscribe สำหรับ count command
	mqttClient.Subscribe(fmt.Sprintf("client/%s/count", clientID), 1, onCountCommand)
	// Subscribe สำหรับ get count request
	mqttClient.Subscribe(fmt.Sprintf("client/%s/get_count", clientID), 1, onGetCountRequest)
	// Subscribe สำหรับ change ID request
	mqttClient.Subscribe(fmt.Sprintf("client/%s/change_id", clientID), 1, onChangeIDRequest)

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
		Count:     count,
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
		SHA256      string `json:"sha256"`
	}
	if err := json.Unmarshal(msg.Payload(), &meta); err != nil {
		fmt.Printf("Error parsing file start: %v\n", err)
		return
	}
	fileName = meta.Filename
	totalChunks = meta.TotalChunks
	expectedHash = meta.SHA256
	fmt.Printf("Receiving file: %s (%d chunks)\n", fileName, totalChunks)
}

// Handler สำหรับ count command
func onCountCommand(client MQTT.Client, msg MQTT.Message) {
	val, err := strconv.Atoi(string(msg.Payload()))
	if err != nil {
		fmt.Printf("Invalid count command: %v\n", err)
		return
	}
	count = val
	fmt.Printf("Count updated to: %d\n", count)
	// ส่งสถานะ count กลับไปที่ server
	ack := map[string]interface{}{
		"client_id": clientID,
		"count":     count,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	ackPayload, _ := json.Marshal(ack)
	mqttClient.Publish("clients/count_ack", 0, false, ackPayload)
}

// Handler สำหรับ get count request
func onGetCountRequest(client MQTT.Client, msg MQTT.Message) {
	fmt.Printf("Server requested current count\n")
	// ส่งค่า count ปัจจุบันกลับไปที่ server
	response := map[string]interface{}{
		"client_id": clientID,
		"count":     count,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	responsePayload, _ := json.Marshal(response)
	mqttClient.Publish("clients/get_count_response", 0, false, responsePayload)
	fmt.Printf("Sent current count (%d) to server\n", count)
}

// Handler สำหรับ change ID request
func onChangeIDRequest(client MQTT.Client, msg MQTT.Message) {
	var request map[string]string
	if err := json.Unmarshal(msg.Payload(), &request); err != nil {
		fmt.Printf("Invalid change ID request: %v\n", err)
		return
	}

	newClientID, exists := request["new_client_id"]
	if !exists {
		fmt.Println("Change ID request missing new_client_id")
		return
	}

	oldClientID := clientID

	// ส่งการยืนยันกลับไปยัง server
	response := map[string]interface{}{
		"old_client_id": oldClientID,
		"new_client_id": newClientID,
		"status":        "success",
	}

	responsePayload, _ := json.Marshal(response)
	client.Publish("clients/change_id_response", 0, false, responsePayload)

	fmt.Printf("Changing client ID from [%s] to [%s]...\n", oldClientID, newClientID)

	// Disconnect และ reconnect ด้วย client ID ใหม่
	client.Disconnect(250)

	// เปลี่ยน client ID
	clientID = newClientID

	// สร้าง connection ใหม่
	opts := MQTT.NewClientOptions().AddBroker("tcp://localhost:1883")
	opts.SetClientID(clientID)
	opts.SetWill("clients/disconnected", clientID, 0, false)

	mqttClient = MQTT.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		fmt.Printf("Failed to reconnect with new ID: %v\n", token.Error())
		return
	}

	// Subscribe ใหม่ด้วย client ID ใหม่
	mqttClient.Subscribe(fmt.Sprintf("file/send/%s/start", clientID), 1, onFileStart)
	mqttClient.Subscribe(fmt.Sprintf("file/send/%s/chunk", clientID), 1, onFileChunk)
	mqttClient.Subscribe(fmt.Sprintf("file/send/%s/end", clientID), 1, onFileEnd)
	mqttClient.Subscribe(fmt.Sprintf("client/%s/count", clientID), 1, onCountCommand)
	mqttClient.Subscribe(fmt.Sprintf("client/%s/get_count", clientID), 1, onGetCountRequest)
	mqttClient.Subscribe(fmt.Sprintf("client/%s/change_id", clientID), 1, onChangeIDRequest)

	// ส่ง status update ด้วย client ID ใหม่
	sendUpdate(mqttClient, clientID)

	fmt.Printf("Successfully changed to client ID: [%s] ✅\n", clientID)
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
	fmt.Println("Received chunk:", index)
	tmpChunks[index] = []byte(parts[1])
}

func onFileEnd(client MQTT.Client, msg MQTT.Message) {
	filePath := filepath.Join("files/received_" + fileName)
	f, err := os.Create(filePath)
	if err != nil {
		fmt.Printf("Error creating file: %v\n", err)
		return
	}
	defer f.Close()

	hash := sha256.New()
	for i := 0; i < totalChunks; i++ {
		if chunk, ok := tmpChunks[i]; ok {
			fmt.Println("Writing chunk:", i)
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

	if expectedHash != "" && sum != expectedHash {
		fmt.Println("⚠️  Warning: Checksum does not match expected hash!")
	} else {
		fmt.Println("✅ Checksum verified.")
	}
}
