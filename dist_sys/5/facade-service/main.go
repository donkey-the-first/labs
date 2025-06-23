package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/consul/api"
	"github.com/segmentio/kafka-go"
)

const (
	maxRetries  = 3
	retryDelay  = time.Second
	consulHost  = "127.0.0.1"
	consulPort  = 8500
	serviceName = "facade-service"
)

type Message struct {
	ID  string `json:"id"`
	Msg string `json:"msg"`
}

var (
	consulClient *api.Client
	appPort      string
)

func main() {
	rand.Seed(time.Now().UnixNano())

	cliPort := flag.Int("port", 0, "Port for the facade service to listen on")
	flag.Parse()

	if *cliPort != 0 {
		appPort = strconv.Itoa(*cliPort)
	} else if envPort := os.Getenv("PORT"); envPort != "" {
		appPort = envPort
	} else {
		appPort = "8080"
	}

	log.Printf("Starting %s on port %s", serviceName, appPort)

	consulConfig := api.DefaultConfig()
	consulConfig.Address = fmt.Sprintf("%s:%d", consulHost, consulPort)
	var err error
	consulClient, err = api.NewClient(consulConfig)
	if err != nil {
		log.Fatalf("Failed to create Consul client: %v", err)
	}

	serviceIP, err := getLocalIP()
	if err != nil {
		log.Printf("Error getting local IP address: %v. Using '127.0.0.1' for registration (may be incorrect in Docker)", err)
		serviceIP = "127.0.0.1"
	}

	serviceID := fmt.Sprintf("%s-%s-%d", serviceName, serviceIP, time.Now().UnixNano())

	registration := &api.AgentServiceRegistration{
		ID:      serviceID,
		Name:    serviceName,
		Port:    mustParseInt(appPort),
		Address: serviceIP,
		Check: &api.AgentServiceCheck{
			HTTP:     fmt.Sprintf("http://%s:%s/health", serviceIP, appPort),
			Interval: "10s",
			Timeout:  "5s",
		},
	}

	err = consulClient.Agent().ServiceRegister(registration)
	if err != nil {
		log.Fatalf("Error registering service '%s' with Consul: %v", serviceName, err)
	}
	log.Printf("Service '%s' (ID: %s) successfully registered with Consul on %s:%s", serviceName, serviceID, registration.Address, appPort)

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/message", messageHandler)
	http.HandleFunc("/messages", messagesHandler)
	http.HandleFunc("/health", healthCheckHandler)

	server := &http.Server{
		Addr: ":" + appPort,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error starting server: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan

	log.Println("Received termination signal, deregistering service from Consul and shutting down server...")

	err = consulClient.Agent().ServiceDeregister(serviceID)
	if err != nil {
		log.Printf("Error deregistering service '%s' (ID: %s) from Consul: %v", serviceName, serviceID, err)
	} else {
		log.Printf("Service '%s' (ID: %s) successfully deregistered from Consul.", serviceName, serviceID)
	}

	ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctxTimeout); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}
	log.Println("Facade Service finished.")
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	response := `Доступні ендпоінти:
- POST /message: Надіслати повідомлення до Kafka (та logging-service)
- GET /messages: Отримати всі повідомлення від messages-service (який читає їх з Kafka)
- GET /health: Перевірка стану сервісу для Consul
`
	fmt.Fprintf(w, response)
}

func getServiceAddressesFromConsul(serviceName string) ([]string, error) {
	services, _, err := consulClient.Catalog().Service(serviceName, "", nil)
	if err != nil {
		return nil, fmt.Errorf("error getting service '%s' from Consul: %w", serviceName, err)
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("no instances found for service '%s' in Consul", serviceName)
	}

	var addresses []string
	for _, service := range services {
		addresses = append(addresses, fmt.Sprintf("%s:%d", service.Address, service.ServicePort))
	}
	return addresses, nil
}

func getKafkaBrokersFromConsul(client *api.Client) ([]string, error) {
	kvPair, _, err := client.KV().Get("config/kafka/brokers", nil)
	if err != nil {
		return nil, fmt.Errorf("error getting 'config/kafka/brokers' from Consul KV: %w", err)
	}
	if kvPair == nil || len(kvPair.Value) == 0 {
		return nil, fmt.Errorf("no 'config/kafka/brokers' found in Consul KV")
	}

	brokersStr := string(kvPair.Value)
	brokers := strings.Split(brokersStr, ",")
	for i, b := range brokers {
		brokers[i] = strings.TrimSpace(b)
	}
	return brokers, nil
}

func getConsulKV(key string) (string, error) {
	kvPair, _, err := consulClient.KV().Get(key, nil)
	if err != nil {
		return "", fmt.Errorf("error getting key '%s' from Consul KV: %w", key, err)
	}
	if kvPair == nil || len(kvPair.Value) == 0 {
		return "", fmt.Errorf("key '%s' not found or empty in Consul KV", key)
	}
	return string(kvPair.Value), nil
}

func messageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}

	var incomingPayload struct {
		ID  interface{} `json:"id"`
		Msg string      `json:"msg"`
	}
	if err := json.Unmarshal(body, &incomingPayload); err != nil {
		http.Error(w, "Invalid JSON payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	id := uuid.New().String()
	message := Message{ID: id, Msg: incomingPayload.Msg}
	payload, err := json.Marshal(message)
	if err != nil {
		http.Error(w, "Error marshaling message", http.StatusInternalServerError)
		return
	}

	kafkaBrokers, err := getKafkaBrokersFromConsul(consulClient)
	if err != nil {
		log.Printf("Failed to get Kafka brokers from Consul: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get Kafka brokers: %v", err), http.StatusInternalServerError)
		return
	}

	kafkaTopic, err := getConsulKV("config/kafka/topic")
	if err != nil {
		log.Printf("Failed to get Kafka topic from Consul KV: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get Kafka topic: %v", err), http.StatusInternalServerError)
		return
	}

	writer := &kafka.Writer{
		Addr:                   kafka.TCP(kafkaBrokers...),
		Topic:                  kafkaTopic,
		Balancer:               &kafka.LeastBytes{},
		RequiredAcks:           kafka.RequireAll,
		AllowAutoTopicCreation: true,
	}
	defer writer.Close()

	err = writer.WriteMessages(context.Background(), kafka.Message{Value: payload})
	if err != nil {
		log.Printf("Error writing message to Kafka: %v", err)
		http.Error(w, fmt.Sprintf("Error writing message to Kafka: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Successfully wrote message with ID %s to Kafka topic '%s'", id, kafkaTopic)

	loggingServiceAddresses, err := getServiceAddressesFromConsul("logging-service")
	if err != nil {
		log.Printf("Failed to get logging service addresses from Consul: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get logging service addresses: %v", err), http.StatusInternalServerError)
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

	if !loggingSuccess {
		http.Error(w, fmt.Sprintf("Failed to send message to logging-service: %v", !loggingSuccess), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Message sent with ID: %s to Kafka and logging-service (%s)", id, selectedLoggingService)
}

func messagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	loggingServiceAddresses, err := getServiceAddressesFromConsul("logging-service")
	if err != nil {
		log.Printf("Failed to get logging service addresses from Consul: %v", err)
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

	logsBody, err := io.ReadAll(logResp.Body)
	if err != nil {
		log.Printf("Error reading response from logging-service: %v", err)
		http.Error(w, "Error reading logs", http.StatusInternalServerError)
		return
	}

	log.Printf("Raw response from logging-service: %s", string(logsBody))

	logMessages := make([]Message, 0)
	if len(logsBody) > 0 && string(logsBody) != "null" {
		if err := json.Unmarshal(logsBody, &logMessages); err != nil {
			var tempWrapper struct {
				ID  string `json:"id"`
				Msg string `json:"msg"`
			}
			if errWrapper := json.Unmarshal(logsBody, &tempWrapper); errWrapper == nil && tempWrapper.ID == "N/A" {
				log.Printf("Warning: Received wrapped log response. Attempting to unmarshal inner JSON string. Raw: %s", tempWrapper.Msg)
				if errInner := json.Unmarshal([]byte(tempWrapper.Msg), &logMessages); errInner != nil {
					log.Printf("Error unmarshaling inner log JSON: %v (raw: %s)", errInner, tempWrapper.Msg)
					logMessages = append(logMessages, Message{ID: "N/A", Msg: string(logsBody)})
				}
			} else {
				log.Printf("Error unmarshaling logs from logging-service (expected JSON array): %v (raw: %s)", err, string(logsBody))
				logMessages = append(logMessages, Message{ID: "N/A", Msg: string(logsBody)})
			}
		}
	} else {
		log.Println("Logging-service returned empty or no logs.")
	}

	messagesServiceAddresses, err := getServiceAddressesFromConsul("messages-service")
	if err != nil {
		log.Printf("Failed to get messages service addresses from Consul: %v", err)
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

	messages, err := io.ReadAll(msgResp.Body)
	if err != nil {
		log.Printf("Error reading response from messages-service: %v", err)
		http.Error(w, "Error reading messages", http.StatusInternalServerError)
		return
	}

	log.Printf("Raw response from messages-service: %s", string(messages))

	var msgMessages []Message
	if len(messages) > 0 && string(messages) != "null" && string(messages) != "[]" {
		if err := json.Unmarshal(messages, &msgMessages); err != nil {
			log.Printf("Error unmarshaling messages from messages-service: %v (raw: %s)", err, string(messages))
			http.Error(w, "Error parsing messages from messages-service", http.StatusInternalServerError)
			return
		}
	} else {
		log.Println("Messages-service returned empty or no messages.")
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

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "UP"})
}

func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}
	return "", fmt.Errorf("could not find a non-loopback IPv4 address")
}

func mustParseInt(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalf("Failed to parse port string '%s' to int: %v", s, err)
	}
	return i
}
