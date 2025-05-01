package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

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

func main() {
	opts := MQTT.NewClientOptions().AddBroker("tcp://broker.emqx.io:1883")
	opts.SetClientID("mqtt-server")

	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	// Subscribe to client status updates
	if token := client.Subscribe("clients/status", 0, handleStatusUpdate); token.Wait() && token.Error() != nil {
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
			clientsMu.RLock()
			defer clientsMu.RUnlock()

			if len(parts) == 1 {
				fmt.Println("\nAll Timestamps:")
				for id, ts := range clients {
					fmt.Printf("- %s: %s\n", id, ts)
				}
			} else {
				clientID := parts[1]
				if ts, exists := clients[clientID]; exists {
					fmt.Printf("\n%s: %s\n", clientID, ts)
				} else {
					fmt.Printf("\nClient %s not found\n", clientID)
				}
			}

		default:
			fmt.Println("Invalid command. Available: list, get [client_id]")
		}
	}
}