package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"sync"
)

var (
	addresses   []string
	addressesMu sync.RWMutex
)

func main() {
	http.HandleFunc("/logging-service", func(w http.ResponseWriter, r *http.Request) {
		addressesMu.RLock()
		defer addressesMu.RUnlock()

		currentAddresses := make([]string, len(addresses))
		copy(currentAddresses, addresses)

		json.NewEncoder(w).Encode(map[string][]string{"addresses": currentAddresses})
	})

	http.HandleFunc("/update-addresses", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
			return
		}

		var requestBody struct {
			Addresses []string `json:"addresses"`
		}

		err := json.NewDecoder(r.Body).Decode(&requestBody)
		if err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}

		addressesMu.Lock()
		defer addressesMu.Unlock()

		addresses = requestBody.Addresses

		sort.Strings(addresses)

		log.Printf("Addresses updated to: %v", addresses)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "Addresses updated successfully"})
	})

	log.Println("Config server running on port 7201")
	if err := http.ListenAndServe(":7201", nil); err != nil {
		log.Fatalf("Failed to start config server: %v", err)
	}
}
