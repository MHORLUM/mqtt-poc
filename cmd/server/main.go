package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

type ClientStatus struct {
	ClientID  string `json:"client_id"`
	Timestamp string `json:"timestamp"`
	Count     int    `json:"count"`
}

type FileAck struct {
	ClientID string `json:"client_id"`
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
}

var (
	clients      = make(map[string]ClientStatus) // เปลี่ยนจาก string เป็น ClientStatus
	clientsMu    sync.RWMutex
	pendingAcks  = make(map[string]string)
	acksReceived = make(map[string]string)
	acksMu       sync.Mutex
)

const InactiveThreshold = 10 * time.Second
const ChunkSize = 64 * 1024 // 64KB

func cleanupInactiveClients() {
	for {
		time.Sleep(5 * time.Second)
		clientsMu.Lock()
		for id, status := range clients {
			ts, _ := time.Parse(time.RFC3339, status.Timestamp)
			if time.Since(ts) > InactiveThreshold {
				delete(clients, id)
				fmt.Printf("Removed inactive client: %s\n", id)
			}
		}
		clientsMu.Unlock()
	}
}

func main() {
	go cleanupInactiveClients()
	opts := MQTT.NewClientOptions().AddBroker("tcp://localhost:1883")
	opts.SetClientID("mqtt-server")
	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	if token := client.Subscribe("clients/status", 0, handleStatusUpdate); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	if token := client.Subscribe("clients/disconnected", 0, handleDisconnect); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	if token := client.Subscribe("clients/ack", 0, handleAck); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	// Subscribe สำหรับ count acknowledgments
	if token := client.Subscribe("clients/count_ack", 0, handleCountAck); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	fmt.Println("Server started. Commands: list, get [client_id], upload [file], count [client_id] [value]")
	startCLI(client)
}

func handleStatusUpdate(client MQTT.Client, msg MQTT.Message) {
	var status ClientStatus
	if err := json.Unmarshal(msg.Payload(), &status); err != nil {
		fmt.Printf("Error decoding message: %v\n", err)
		return
	}
	clientsMu.Lock()
	clients[status.ClientID] = status
	clientsMu.Unlock()
}

func handleDisconnect(_ MQTT.Client, msg MQTT.Message) {
	clientID := string(msg.Payload())
	clientsMu.Lock()
	delete(clients, clientID)
	clientsMu.Unlock()
	fmt.Printf("Client %s disconnected\n", clientID)
}

func handleAck(_ MQTT.Client, msg MQTT.Message) {
	var ack FileAck
	if err := json.Unmarshal(msg.Payload(), &ack); err != nil {
		fmt.Printf("Invalid ACK: %v\n", err)
		return
	}

	acksMu.Lock()
	acksReceived[ack.ClientID] = ack.SHA256
	expected, exists := pendingAcks[ack.ClientID]
	acksMu.Unlock()

	if exists {
		if ack.SHA256 == expected {
			fmt.Printf("ACK verified from [%s] ✅\n", ack.ClientID)
		} else {
			fmt.Printf("Checksum mismatch from [%s] ❌\n", ack.ClientID)
		}
	}
}

func handleCountAck(_ MQTT.Client, msg MQTT.Message) {
	var ack map[string]interface{}
	if err := json.Unmarshal(msg.Payload(), &ack); err != nil {
		fmt.Printf("Invalid count ACK: %v\n", err)
		return
	}

	clientID, _ := ack["client_id"].(string)
	count, _ := ack["count"].(float64) // JSON numbers เป็น float64
	timestamp, _ := ack["timestamp"].(string)

	fmt.Printf("Count ACK from [%s]: %d at %s ✅\n", clientID, int(count), timestamp)
}

func startCLI(mqttClient MQTT.Client) {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		parts := strings.SplitN(input, " ", 2)
		command := parts[0]

		switch command {
		case "list":
			clientsMu.RLock()
			fmt.Println("\nConnected Clients:")
			for id, status := range clients {
				fmt.Printf("- %s (count: %d)\n", id, status.Count)
			}
			clientsMu.RUnlock()

		case "get":
			targetClient := ""
			if len(parts) > 1 {
				targetClient = parts[1]
			}
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT)
			defer signal.Stop(sigChan)
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			fmt.Println("Starting live updates (Ctrl+C to exit)...")
			for {
				select {
				case <-ticker.C:
					clientsMu.RLock()
					if targetClient == "" {
						fmt.Println("\n=== All Client Status ===")
						for id, status := range clients {
							fmt.Printf("- %s: %s (count: %d)\n", id, status.Timestamp, status.Count)
						}
					} else {
						if status, ok := clients[targetClient]; ok {
							fmt.Printf("\n[%s]: %s (count: %d)\n", targetClient, status.Timestamp, status.Count)
						} else {
							fmt.Printf("\nClient %s not found\n", targetClient)
						}
					}
					clientsMu.RUnlock()
				case <-sigChan:
					fmt.Println("\nExiting live updates")
					return
				}
			}

		case "upload":
			if len(parts) < 2 {
				fmt.Println("Usage: upload [file_path]")
				continue
			}
			sendFileToAllClients(mqttClient, parts[1])

		case "count":
			if len(parts) < 2 {
				fmt.Println("Usage: count [client_id] [value]")
				continue
			}
			args := strings.Fields(parts[1])
			if len(args) != 2 {
				fmt.Println("Usage: count [client_id] [value]")
				continue
			}
			targetClient := args[0]
			value := args[1]
			mqttClient.Publish(fmt.Sprintf("client/%s/count", targetClient), 0, false, value)
			fmt.Printf("Sent count command to [%s]: %s\n", targetClient, value)

		default:
			fmt.Println("Invalid command. Available: list, get [client_id], upload [file], count [client_id] [value]")
		}
	}
}

func sendFileToAllClients(mqttClient MQTT.Client, filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Failed to read file: %v\n", err)
		return
	}

	filename := filepath.Base(filePath)
	sum := sha256.Sum256(data)
	checksum := hex.EncodeToString(sum[:])
	totalChunks := (len(data) + ChunkSize - 1) / ChunkSize

	clientsMu.RLock()
	defer clientsMu.RUnlock()

	acksMu.Lock()
	pendingAcks = make(map[string]string)
	acksMu.Unlock()

	var wg sync.WaitGroup

	fmt.Printf("Uploading %s (%d chunks) to %d clients...\n", filename, totalChunks, len(clients))
	for clientID := range clients {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()

			fmt.Printf("Sending to [%s]...\n", id)

			acksMu.Lock()
			pendingAcks[id] = checksum
			acksMu.Unlock()

			meta := map[string]interface{}{
				"filename":     filename,
				"total_chunks": totalChunks,
				"sha256":       checksum,
			}
			payload, _ := json.Marshal(meta)
			mqttClient.Publish(fmt.Sprintf("file/send/%s/start", id), 0, false, payload)

			for i := 0; i < totalChunks; i++ {
				start := i * ChunkSize
				end := start + ChunkSize
				if end > len(data) {
					end = len(data)
				}
				chunk := data[start:end]
				head := fmt.Sprintf("%d/", i)
				mqttClient.Publish(fmt.Sprintf("file/send/%s/chunk", id), 0, false, append([]byte(head), chunk...))
				time.Sleep(50 * time.Millisecond)
			}
			mqttClient.Publish(fmt.Sprintf("file/send/%s/end", id), 0, false, []byte("done"))
			fmt.Printf("Finished sending to [%s]\n", id)
		}(clientID)
	}

	wg.Wait()
	fmt.Println("All uploads dispatched. Waiting for ACKs...")
}
