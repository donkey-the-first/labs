package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/segmentio/kafka-go"
)

const (
	consulHost  = "127.0.0.1"
	consulPort  = 8500
	serviceName = "messages-service"
)

type Message struct {
	ID  string `json:"id"`
	Msg string `json:"msg"`
}

var (
	messages     = make([]Message, 0)
	appPort      string
	consulClient *api.Client
)

func main() {
	cliPort := flag.Int("port", 0, "Port for the messages service to listen on")
	flag.Parse()

	if *cliPort != 0 {
		appPort = strconv.Itoa(*cliPort)
	} else if envPort := os.Getenv("PORT"); envPort != "" {
		appPort = envPort
	} else {
		appPort = "8072"
	}

	log.Printf("Starting %s on port %s", serviceName, appPort)

	consulConfig := api.DefaultConfig()
	consulConfig.Address = fmt.Sprintf("%s:%d", consulHost, consulPort)
	var err error
	consulClient, err = api.NewClient(consulConfig)
	if err != nil {
		log.Fatalf("Failed to create Consul client: %v", err)
	}

	kafkaBrokers, err := getKafkaBrokersFromConsul(consulClient)
	if err != nil {
		log.Printf("Error getting Kafka brokers from Consul: %v. Kafka consumer might not function.", err)
		kafkaBrokers = []string{}
	} else if len(kafkaBrokers) == 0 {
		log.Println("No Kafka brokers found in Consul. Kafka consumer might not function.")
	} else {
		log.Printf("Kafka brokers from Consul: %v", kafkaBrokers)
	}

	kafkaTopic, err := getConsulKV("config/kafka/topic")
	if err != nil {
		log.Fatalf("Failed to get Kafka topic from Consul KV: %v", err)
	}
	log.Printf("Kafka topic from Consul KV: %s", kafkaTopic)

	baseConsumerGroupID, err := getConsulKV("config/kafka/consumer_group_id")
	if err != nil {
		log.Fatalf("Failed to get Consumer Group ID from Consul KV: %v", err)
	}

	serviceIP, err := getLocalIP()
	if err != nil {
		log.Printf("Error getting local IP address: %v. Using '127.0.0.1' for registration (may be incorrect in Docker)", err)
		serviceIP = "127.0.0.1"
	}

	serviceID := fmt.Sprintf("%s-%s-%d", serviceName, serviceIP, time.Now().UnixNano())

	uniqueConsumerGroupID := fmt.Sprintf("%s-%s", baseConsumerGroupID, serviceID)
	log.Printf("Using unique Consumer Group ID: %s", uniqueConsumerGroupID)

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
		log.Fatalf("Failed to register service '%s' with Consul: %v", serviceName, err)
	}
	log.Printf("Service '%s' (ID: %s) successfully registered with Consul on %s:%s", serviceName, serviceID, registration.Address, appPort)

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     kafkaBrokers,
		Topic:       kafkaTopic,
		GroupID:     uniqueConsumerGroupID,
		MinBytes:    10e3,
		MaxBytes:    10e6,
		StartOffset: kafka.FirstOffset,
	})
	defer reader.Close()

	go func() {
		log.Println("Starting Kafka consumer goroutine...")
		if len(kafkaBrokers) == 0 {
			log.Println("Kafka brokers are not configured. Consumer will not start.")
			return
		}
		for {
			msg, err := reader.ReadMessage(context.Background())
			if err != nil {
				log.Printf("Error reading from Kafka: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}
			var message Message
			if err := json.Unmarshal(msg.Value, &message); err != nil {
				log.Printf("Error unmarshaling message from Kafka: %v (raw: %s)", err, string(msg.Value))
				continue
			}
			messages = append(messages, message)
			log.Printf("Received message: ID=%s, Msg=%s", message.ID, message.Msg)
		}
	}()

	http.HandleFunc("/messages", messagesHandler)
	http.HandleFunc("/health", healthCheckHandler)

	server := &http.Server{
		Addr: ":" + appPort,
	}

	go func() {
		log.Printf("HTTP server listening on :%s", appPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error starting server: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan

	log.Println("Received termination signal, starting graceful shutdown...")

	err = consulClient.Agent().ServiceDeregister(serviceID)
	if err != nil {
		log.Printf("Error deregistering service '%s' (ID: %s) from Consul: %v", serviceName, serviceID, err)
	} else {
		log.Printf("Service '%s' (ID: %s) successfully deregistered from Consul.", serviceName, serviceID)
	}

	ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(ctxShutdown); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}
	log.Println("Messages Service finished.")
}

func getKafkaBrokersFromConsul(client *api.Client) ([]string, error) {
	kvPair, _, err := client.KV().Get("config/kafka/brokers", nil)
	if err != nil {
		return nil, fmt.Errorf("error getting 'config/kafka/brokers' from Consul KV: %w", err)
	}
	if kvPair == nil || len(kvPair.Value) == 0 {
		return nil, nil
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

func messagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	response, err := json.Marshal(messages)
	if err != nil {
		log.Printf("Error marshaling messages: %v", err)
		http.Error(w, fmt.Sprintf("Error marshaling messages: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, string(response))
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "UP"})
}
