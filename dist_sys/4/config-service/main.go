package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"sync"
)

type Config struct {
	LoggingAddresses  []string `json:"addresses"`
	MessagesAddresses []string `json:"addresses"`
	KafkaBrokers      []string `json:"kafka_brokers"`
}

var (
	config   Config
	configMu sync.RWMutex
)

func main() {
	config = Config{
		LoggingAddresses:  []string{"localhost:8201", "localhost:8202", "localhost:8203"},
		MessagesAddresses: []string{"localhost:8081", "localhost:8082"},
		KafkaBrokers:      []string{"localhost:8092", "localhost:8093"},
	}

	log.Printf("Initial configuration: LoggingAddresses=%v, MessagesAddresses=%v, KafkaBrokers=%v", config.LoggingAddresses, config.MessagesAddresses, config.KafkaBrokers)

	http.HandleFunc("/logging-service", func(w http.ResponseWriter, r *http.Request) {
		configMu.RLock()
		defer configMu.RUnlock()

		response := map[string][]string{"addresses": make([]string, len(config.LoggingAddresses))}
		copy(response["addresses"], config.LoggingAddresses)
		log.Printf("Returning logging-service addresses: %v", response["addresses"])
		json.NewEncoder(w).Encode(response)
	})

	http.HandleFunc("/messages-service", func(w http.ResponseWriter, r *http.Request) {
		configMu.RLock()
		defer configMu.RUnlock()

		response := map[string][]string{
			"addresses":     make([]string, len(config.MessagesAddresses)),
			"kafka_brokers": make([]string, len(config.KafkaBrokers)),
		}
		copy(response["addresses"], config.MessagesAddresses)
		copy(response["kafka_brokers"], config.KafkaBrokers)
		log.Printf("Returning messages-service addresses: %v, KafkaBrokers: %v", response["addresses"], response["kafka_brokers"])
		json.NewEncoder(w).Encode(response)
	})

	http.HandleFunc("/update-addresses", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
			return
		}

		var requestBody struct {
			LoggingAddresses  []string `json:"logging_addresses"`
			MessagesAddresses []string `json:"messages_addresses"`
			KafkaBrokers      []string `json:"kafka_brokers"`
		}

		err := json.NewDecoder(r.Body).Decode(&requestBody)
		if err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}

		configMu.Lock()
		defer configMu.Unlock()

		if len(requestBody.LoggingAddresses) > 0 {
			config.LoggingAddresses = requestBody.LoggingAddresses
			sort.Strings(config.LoggingAddresses)
		}
		if len(requestBody.MessagesAddresses) > 0 {
			config.MessagesAddresses = requestBody.MessagesAddresses
			sort.Strings(config.MessagesAddresses)
		}
		if len(requestBody.KafkaBrokers) > 0 {
			config.KafkaBrokers = requestBody.KafkaBrokers
			sort.Strings(config.KafkaBrokers)
		}

		log.Printf("Configuration updated: LoggingAddresses=%v, MessagesAddresses=%v, KafkaBrokers=%v", config.LoggingAddresses, config.MessagesAddresses, config.KafkaBrokers)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "Configuration updated successfully"})
	})

	log.Println("Config server running on port 7201")
	if err := http.ListenAndServe(":7201", nil); err != nil {
		log.Fatalf("Failed to start config server: %v", err)
	}
}
