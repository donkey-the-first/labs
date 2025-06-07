package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"time"
)

const maxRetries = 3
const retryDelay = time.Second
const configServiceURL = "http://localhost:7201/logging-service"

type ConfigServiceResponse struct {
	Addresses []string `json:"addresses"`
}

func main() {
	// Ініціалізація генератора випадкових чисел
	rand.Seed(time.Now().UnixNano())

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/message", messageHandler)
	http.HandleFunc("/messages", messagesHandler)

	log.Println("Starting facade-service on port 8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	response := `Доступні ендпоінти:
- POST /message: Надіслати повідомлення у logging-service
- GET /messages: Отримати всі повідомлення (від logging-service та messages-service)
`
	fmt.Fprintf(w, response)
}

func getLoggingServiceAddresses() ([]string, error) {
	resp, err := http.Get(configServiceURL)
	if err != nil {
		return nil, fmt.Errorf("error getting addresses from config-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("config-service returned non-OK status: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading config-service response body: %w", err)
	}

	var configResp ConfigServiceResponse
	err = json.Unmarshal(body, &configResp)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling config-service response: %w", err)
	}

	if len(configResp.Addresses) == 0 {
		return nil, fmt.Errorf("no logging service addresses found from config-service")
	}

	return configResp.Addresses, nil
}

func messageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	msg := string(body)

	id := uuid.New().String()
	payload := fmt.Sprintf(`{"id": "%s", "msg": "%s"}`, id, msg)

	loggingServiceAddresses, err := getLoggingServiceAddresses()
	if err != nil {
		log.Printf("Failed to get logging service addresses: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get logging service addresses: %v", err), http.StatusInternalServerError)
		return
	}

	selectedLoggingService := ""

	var resp *http.Response
	successfulAttempt := false
	for attempt := 1; attempt <= maxRetries; attempt++ {
		randomIndex := rand.Intn(len(loggingServiceAddresses))
		selectedLoggingService := loggingServiceAddresses[randomIndex]
		loggingServiceLogURL := fmt.Sprintf("http://%s/log", selectedLoggingService)
		log.Printf("Attempt %d: Sending POST request to %s with payload: %s\n", attempt, loggingServiceLogURL, payload)
		resp, err = http.Post(loggingServiceLogURL, "application/json", bytes.NewBuffer([]byte(payload)))
		if err == nil && resp.StatusCode == http.StatusOK {
			log.Printf("Successfully sent message on attempt %d to %s\n", attempt, selectedLoggingService)
			successfulAttempt = true
			break
		}
		if err != nil {
			log.Printf("Error on attempt %d to %s: %v\n", attempt, selectedLoggingService, err)
		} else {
			log.Printf("Logging-service %s returned non-OK status on attempt %d: %s\n", selectedLoggingService, attempt, resp)
		}
		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}

	if !successfulAttempt {
		http.Error(w, "Failed to send message after retries to any logging-service", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	fmt.Fprintf(w, "Message sent with ID: %s to %s", id, selectedLoggingService)
}

func messagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	loggingServiceAddresses, err := getLoggingServiceAddresses()
	if err != nil {
		log.Printf("Failed to get logging service addresses for GET /messages: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get logging service addresses: %v", err), http.StatusInternalServerError)
		return
	}

	if len(loggingServiceAddresses) == 0 {
		http.Error(w, "No logging services available to fetch logs from", http.StatusInternalServerError)
		return
	}

	randomIndex := rand.Intn(len(loggingServiceAddresses))
	selectedLoggingServiceForLogs := loggingServiceAddresses[randomIndex]

	loggingServiceLogsURL := fmt.Sprintf("http://%s/logs", selectedLoggingServiceForLogs)
	log.Printf("Sending GET request to logging-service at %s", loggingServiceLogsURL)

	logResp, err := http.Get(loggingServiceLogsURL)
	if err != nil {
		log.Printf("Error sending GET request to logging-service %s: %v\n", loggingServiceLogsURL, err)
		http.Error(w, "Error getting logs from logging-service", http.StatusInternalServerError)
		return
	}
	defer logResp.Body.Close()

	log.Printf("Received response from logging-service %s: %d\n", selectedLoggingServiceForLogs, logResp.StatusCode)

	logs, err := ioutil.ReadAll(logResp.Body)
	if err != nil {
		log.Printf("Error reading response from logging-service: %v\n", err)
		http.Error(w, "Error reading logs", http.StatusInternalServerError)
		return
	}

	log.Println("Sending GET request to messages-service at http://localhost:8082/message")

	msgResp, err := http.Get("http://localhost:8082/message")
	if err != nil {
		log.Printf("Error sending GET request to messages-service: %v\n", err)
		http.Error(w, "Error getting message", http.StatusInternalServerError)
		return
	}
	defer msgResp.Body.Close()

	log.Printf("Received response from messages-service: %d\n", msgResp.StatusCode)

	message, err := ioutil.ReadAll(msgResp.Body)
	if err != nil {
		log.Printf("Error reading response from messages-service: %v\n", err)
		http.Error(w, "Error reading message", http.StatusInternalServerError)
		return
	}

	response := string(logs) + "\n" + string(message)
	fmt.Fprintf(w, response)
}
