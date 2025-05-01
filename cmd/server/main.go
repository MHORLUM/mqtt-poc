package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

type ClientStatus struct {
	ClientID  string `json:"client_id"`
	Timestamp string `json:"timestamp"`
}

var (
	clients   = make(map[string]string)
	clientsMu sync.RWMutex
)

const InactiveThreshold = 10 * time.Second

func cleanupInactiveClients() {
	for {
		time.Sleep(5 * time.Second)

		clientsMu.Lock()
		for id, tsStr := range clients {
			ts, _ := time.Parse(time.RFC3339, tsStr)
			if time.Since(ts) > InactiveThreshold {
				delete(clients, id)
				fmt.Printf("Removed inactive client: %s\n", id)
			}
		}
		clientsMu.Unlock()
	}
}

func main() {
	// Start a goroutine to clean up inactive clients
	go cleanupInactiveClients()
	opts := MQTT.NewClientOptions().AddBroker("tcp://localhost:1883")
	opts.SetClientID("mqtt-server")

	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	// Subscribe to client status updates
	if token := client.Subscribe("clients/status", 0, handleStatusUpdate); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	if token := client.Subscribe("clients/disconnected", 0, handleDisconnect); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	fmt.Println("Server started. Commands: list, get [client_id]")
	startCLI()
}

func handleStatusUpdate(client MQTT.Client, msg MQTT.Message) {
	var status ClientStatus
	if err := json.Unmarshal(msg.Payload(), &status); err != nil {
		fmt.Printf("Error decoding message: %v\n", err)
		return
	}

	clientsMu.Lock()
	clients[status.ClientID] = status.Timestamp
	clientsMu.Unlock()
}

func handleDisconnect(_ MQTT.Client, msg MQTT.Message) {
	clientID := string(msg.Payload())

	clientsMu.Lock()
	delete(clients, clientID) // ลบ Client ออกจาก Map
	clientsMu.Unlock()

	fmt.Printf("Client %s disconnected\n", clientID)
}

func startCLI() {
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
			for id := range clients {
				fmt.Println("-", id)
			}
			clientsMu.RUnlock()

		case "get":
			targetClient := ""
			if len(parts) > 1 {
				targetClient = parts[1]
			}

			// ตั้งค่า Channel สำหรับรับ Signal (Ctrl+C)
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT)
			defer signal.Stop(sigChan)

			ticker := time.NewTicker(1 * time.Second) // อัปเดตทุก 1 วินาที
			defer ticker.Stop()

			fmt.Println("Starting live updates (Ctrl+C to exit)...")
			for {
				select {
				case <-ticker.C:
					clientsMu.RLock()
					if targetClient == "" {
						fmt.Println("\n=== All Timestamps ===")
						for id, ts := range clients {
							fmt.Printf("- %s: %s\n", id, ts)
						}
					} else {
						if ts, ok := clients[targetClient]; ok {
							fmt.Printf("\n[%s]: %s\n", targetClient, ts)
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
		default:
			fmt.Println("Invalid command. Available: list, get [client_id]")
		}
	}
}
