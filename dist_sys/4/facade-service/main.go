package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	maxRetries       = 3
	retryDelay       = time.Second
	configServiceURL = "http://localhost:7201"
)

type ConfigServiceResponse struct {
	Addresses []string `json:"addresses"`
}

type Message struct {
	ID  string `json:"id"`
	Msg string `json:"msg"`
}

type MessageWithNested struct {
	ID  string          `json:"id"`
	Msg json.RawMessage `json:"msg"`
}

func main() {
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
- POST /message: Надіслати повідомлення до messages-service та logging-service
- GET /messages: Отримати всі повідомлення (окремо від logging-service та messages-service)
`
	fmt.Fprintf(w, response)
}

func getServiceAddresses(service string) ([]string, error) {
	url := fmt.Sprintf("%s/%s", configServiceURL, service)
	log.Printf("Attempting to get addresses for %s from %s", service, url)
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("HTTP GET error for %s: %v", url, err)
		return nil, fmt.Errorf("error getting addresses from config-service for %s: %w", service, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Non-OK status for %s: %d", url, resp.StatusCode)
		return nil, fmt.Errorf("config-service returned non-OK status for %s: %d", service, resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body for %s: %v", url, err)
		return nil, fmt.Errorf("error reading config-service response body for %s: %w", service, err)
	}
	log.Printf("Raw response body for %s: %s", service, string(body))

	var configResp ConfigServiceResponse
	err = json.Unmarshal(body, &configResp)
	if err != nil {
		log.Printf("Error unmarshaling response for %s: %v", url, err)
		return nil, fmt.Errorf("error unmarshaling config-service response for %s: %w", service, err)
	}

	addresses := configResp.Addresses
	log.Printf("Extracted %s addresses: %v", service, addresses)

	if len(addresses) == 0 {
		log.Printf("No addresses found for %s in response: %+v", service, configResp)
		return nil, fmt.Errorf("no %s addresses found from config-service", service)
	}

	return addresses, nil
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
	message := Message{ID: id, Msg: msg}
	payload, err := json.Marshal(message)
	if err != nil {
		http.Error(w, "Error marshaling message", http.StatusInternalServerError)
		return
	}

	loggingServiceAddresses, err := getServiceAddresses("logging-service")
	if err != nil {
		log.Printf("Failed to get logging service addresses: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get logging service addresses: %v", err), http.StatusInternalServerError)
		return
	}

	messagesServiceAddresses, err := getServiceAddresses("messages-service")
	if err != nil {
		log.Printf("Failed to get messages service addresses: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get messages service addresses: %v", err), http.StatusInternalServerError)
		return
	}

	var loggingSuccess bool
	var selectedLoggingService string
	for attempt := 1; attempt <= maxRetries; attempt++ {
		randomIndex := rand.Intn(len(loggingServiceAddresses))
		selectedLoggingService = loggingServiceAddresses[randomIndex]
		loggingServiceLogURL := fmt.Sprintf("http://%s/log", selectedLoggingService)
		log.Printf("Attempt %d: Sending POST request to logging-service %s with payload: %s", attempt, loggingServiceLogURL, payload)
		resp, err := http.Post(loggingServiceLogURL, "application/json", bytes.NewBuffer(payload))
		if err == nil && resp.StatusCode == http.StatusOK {
			log.Printf("Successfully sent message to logging-service %s on attempt %d", selectedLoggingService, attempt)
			loggingSuccess = true
			break
		}
		if err != nil {
			log.Printf("Error on attempt %d to logging-service %s: %v", attempt, selectedLoggingService, err)
		} else {
			log.Printf("Logging-service %s returned non-OK status on attempt %d: %s", selectedLoggingService, attempt, resp.Status)
		}
		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}

	var messagesSuccess bool
	var selectedMessagesService string
	for attempt := 1; attempt <= maxRetries; attempt++ {
		randomIndex := rand.Intn(len(messagesServiceAddresses))
		selectedMessagesService = messagesServiceAddresses[randomIndex]
		messagesServiceURL := fmt.Sprintf("http://%s/message", selectedMessagesService)
		log.Printf("Attempt %d: Sending POST request to messages-service %s with payload: %s", attempt, messagesServiceURL, payload)
		resp, err := http.Post(messagesServiceURL, "application/json", bytes.NewBuffer(payload))
		if err == nil && resp.StatusCode == http.StatusOK {
			log.Printf("Successfully sent message to messages-service %s on attempt %d", selectedMessagesService, attempt)
			messagesSuccess = true
			break
		}
		if err != nil {
			log.Printf("Error on attempt %d to messages-service %s: %v", attempt, selectedMessagesService, err)
		} else {
			log.Printf("Messages-service %s returned non-OK status on attempt %d: %s", selectedMessagesService, attempt, resp.Status)
		}
		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}

	if !loggingSuccess || !messagesSuccess {
		http.Error(w, fmt.Sprintf("Failed to send message to logging-service: %v, messages-service: %v", !loggingSuccess, !messagesSuccess), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Message sent with ID: %s to logging-service (%s) and messages-service (%s)", id, selectedLoggingService, selectedMessagesService)
}

func messagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	loggingServiceAddresses, err := getServiceAddresses("logging-service")
	if err != nil {
		log.Printf("Failed to get logging service addresses: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get logging service addresses: %v", err), http.StatusInternalServerError)
		return
	}

	loggingIndex := rand.Intn(len(loggingServiceAddresses))
	loggingURL := fmt.Sprintf("http://%s/logs", loggingServiceAddresses[loggingIndex])
	log.Printf("Sending GET request to logging-service at %s", loggingURL)

	logResp, err := http.Get(loggingURL)
	if err != nil {
		log.Printf("Error sending GET request to logging-service %s: %v", loggingURL, err)
		http.Error(w, "Error getting logs from logging-service", http.StatusInternalServerError)
		return
	}
	defer logResp.Body.Close()

	logs, err := ioutil.ReadAll(logResp.Body)
	if err != nil {
		log.Printf("Error reading response from logging-service: %v", err)
		http.Error(w, "Error reading logs", http.StatusInternalServerError)
		return
	}

	messagesServiceAddresses, err := getServiceAddresses("messages-service")
	if err != nil {
		log.Printf("Failed to get messages service addresses: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get messages service addresses: %v", err), http.StatusInternalServerError)
		return
	}

	messagesIndex := rand.Intn(len(messagesServiceAddresses))
	messagesURL := fmt.Sprintf("http://%s/messages", messagesServiceAddresses[messagesIndex])
	log.Printf("Sending GET request to messages-service at %s", messagesURL)

	msgResp, err := http.Get(messagesURL)
	if err != nil {
		log.Printf("Error sending GET request to messages-service %s: %v", messagesURL, err)
		http.Error(w, "Error getting messages from messages-service", http.StatusInternalServerError)
		return
	}
	defer msgResp.Body.Close()

	messages, err := ioutil.ReadAll(msgResp.Body)
	if err != nil {
		log.Printf("Error reading response from messages-service: %v", err)
		http.Error(w, "Error reading messages", http.StatusInternalServerError)
		return
	}

	logMessages := make([]Message, 0)
	logLines := strings.Split(string(logs), "\n")
	for _, line := range logLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err == nil {
			logMessages = append(logMessages, msg)
		} else {
			log.Printf("Error unmarshaling log line into Message: %v (raw line: '%s')", err, line)
		}
	}

	log.Printf("Raw response from messages-service: %s", string(messages))

	var msgMessages []Message
	if err := json.Unmarshal(messages, &msgMessages); err != nil {
		log.Printf("Error unmarshaling messages from messages-service: %v (raw: %s)", err, string(messages))
		if len(messages) > 0 && string(messages) != "[]" {
			http.Error(w, "Error parsing messages from messages-service", http.StatusInternalServerError)
			return
		}
	}

	response := fmt.Sprintf("Messages from logging-service (%s):\n", loggingServiceAddresses[loggingIndex])
	for _, m := range logMessages {
		jsonData, err := json.Marshal(m)
		if err != nil {
			log.Printf("Error marshaling log message: %v", err)
			continue
		}
		response += fmt.Sprintf("%s\n", string(jsonData))
	}
	response += fmt.Sprintf("\nMessages from messages-service (%s):\n", messagesServiceAddresses[messagesIndex])
	for _, m := range msgMessages {
		jsonData, err := json.Marshal(m)
		if err != nil {
			log.Printf("Error marshaling message: %v", err)
			continue
		}
		response += fmt.Sprintf("%s\n", string(jsonData))
	}
	fmt.Fprintf(w, response)
}
