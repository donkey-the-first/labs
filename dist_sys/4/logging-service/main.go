package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	hazelcast "github.com/hazelcast/hazelcast-go-client"
)

type LogMessage struct {
	ID  string `json:"id"`
	Msg string `json:"msg"`
}

var (
	messagesMap *hazelcast.Map
)

func main() {
	servicePort := flag.Int("port", 8081, "Port for the logging service to listen on")
	hazelcastAddresses := flag.String("hazelcast-addresses", "127.0.0.1:5701", "Comma-separated list of Hazelcast cluster addresses (e.g., 127.0.0.1:5701,127.0.0.2:5701)")
	mapName := flag.String("map", "my-logs", "Name of the Hazelcast distributed map")

	flag.Parse()

	ctx := context.Background()

	addresses := strings.Split(*hazelcastAddresses, ",")

	config := hazelcast.NewConfig()
	config.Cluster.Name = "dev-map"
	config.Cluster.Network.SetAddresses(addresses...)

	client, err := hazelcast.StartNewClientWithConfig(ctx, config)
	if err != nil {
		fmt.Printf("Failed to connect to Hazelcast cluster: %v\n", err)
		return
	}
	defer func() {
		fmt.Println("Shutting down Hazelcast client...")
		if err := client.Shutdown(ctx); err != nil {
			fmt.Printf("Error shutting down Hazelcast client: %v\n", err)
		}
	}()

	messagesMap, err = client.GetMap(ctx, *mapName)
	if err != nil {
		fmt.Printf("Failed to get distributed map '%s': %v\n", *mapName, err)
		return
	}
	fmt.Printf("Successfully connected to Hazelcast map '%s' at addresses %v\n", *mapName, addresses)

	http.HandleFunc("/log", logHandler)
	http.HandleFunc("/logs", logsHandler)

	listenAddr := fmt.Sprintf(":%d", *servicePort)
	fmt.Printf("Starting logging-service on port %s\n", listenAddr)
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		fmt.Printf("Error starting server: %v\n", err)
		return
	}
}

func logHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}

	var data LogMessage
	err = json.Unmarshal(body, &data)
	if err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if data.ID == "" || data.Msg == "" {
		http.Error(w, "Missing id or msg in JSON payload", http.StatusBadRequest)
		return
	}

	ctx := context.Background()

	_, err = messagesMap.PutIfAbsent(ctx, data.ID, data.Msg)
	if err != nil {
		fmt.Printf("Error putting message to Hazelcast: %v\n", err)
		http.Error(w, "Failed to log message to distributed map", http.StatusInternalServerError)
		return
	}

	exists, err := messagesMap.ContainsKey(ctx, data.ID)
	if err != nil {
		fmt.Printf("Error checking existence of key %s in Hazelcast: %v\n", data.ID, err)
		http.Error(w, "Failed to verify message existence", http.StatusInternalServerError)
		return
	}

	if exists {
		fmt.Printf("Received message: ID=%s, Msg=%s. Already exists or successfully added to map.\n", data.ID, data.Msg)
	} else {
		fmt.Printf("Received and saved new message: ID=%s, Msg=%s\n", data.ID, data.Msg)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Message logged successfully with ID: %s", data.ID)
}

func logsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	ctx := context.Background()
	entryValues, err := messagesMap.GetEntrySet(ctx)
	if err != nil {
		fmt.Printf("Error getting messages from Hazelcast: %v\n", err)
		http.Error(w, "Failed to retrieve messages from distributed map", http.StatusInternalServerError)
		return
	}

	var allMessages []string
	for _, entry := range entryValues {
		if msg, ok := entry.Value.(string); ok {
			allMessages = append(allMessages, msg)
		} else {
			fmt.Printf("Warning: Non-string value found in map for key %v (type %T)\n", entry.Key, entry.Value)
		}
	}

	response := strings.Join(allMessages, "\n")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, response)
}
