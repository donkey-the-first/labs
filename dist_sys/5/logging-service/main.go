package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	"github.com/hazelcast/hazelcast-go-client"
)

const (
	consulHost  = "127.0.0.1"
	consulPort  = 8500
	serviceName = "logging-service"
)

type LogMessage struct {
	ID  string `json:"id"`
	Msg string `json:"msg"`
}

var (
	appPort      string
	consulClient *api.Client
	hzClient     *hazelcast.Client
	messagesMap  *hazelcast.Map
)

func main() {
	cliPort := flag.Int("port", 0, "Port for the logging service to listen on")
	flag.Parse()

	if *cliPort != 0 {
		appPort = strconv.Itoa(*cliPort)
	} else if envPort := os.Getenv("PORT"); envPort != "" {
		appPort = envPort
	} else {
		appPort = "8081"
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
		log.Fatalf("Failed to register service '%s' with Consul: %v", serviceName, err)
	}
	log.Printf("Service '%s' (ID: %s) successfully registered with Consul on %s:%s", serviceName, serviceID, registration.Address, appPort)

	hazelcastAddressesStr, err := getConsulKV("config/hazelcast/addresses")
	if err != nil {
		log.Fatalf("Failed to get Hazelcast addresses from Consul KV: %v", err)
	}
	hazelcastAddresses := strings.Split(hazelcastAddressesStr, ",")
	for i, addr := range hazelcastAddresses {
		hazelcastAddresses[i] = strings.TrimSpace(addr)
	}
	if len(hazelcastAddresses) == 0 {
		log.Fatalf("No Hazelcast addresses configured in Consul KV at 'config/hazelcast/addresses'")
	}

	hazelcastClusterName, err := getConsulKV("config/hazelcast/cluster-name")
	if err != nil {
		log.Fatalf("Failed to get Hazelcast cluster name from Consul KV: %v", err)
	}

	hazelcastMapName, err := getConsulKV("config/hazelcast/map-name")
	if err != nil {
		log.Fatalf("Failed to get Hazelcast map name from Consul KV: %v", err)
	}

	ctx := context.Background()
	hzConfig := hazelcast.NewConfig()
	hzConfig.Cluster.Name = hazelcastClusterName
	hzConfig.Cluster.Network.SetAddresses(hazelcastAddresses...)

	hzClient, err = hazelcast.StartNewClientWithConfig(ctx, hzConfig)
	if err != nil {
		log.Fatalf("Failed to connect to Hazelcast cluster: %v\n", err)
	}
	log.Printf("Successfully connected to Hazelcast cluster '%s'.", hazelcastClusterName)

	messagesMap, err = hzClient.GetMap(ctx, hazelcastMapName)
	if err != nil {
		log.Fatalf("Failed to get distributed map '%s': %v\n", hazelcastMapName, err)
	}
	log.Printf("Successfully connected to Hazelcast map '%s' at addresses %v\n", hazelcastMapName, hazelcastAddresses)

	http.HandleFunc("/log", logHandler)
	http.HandleFunc("/logs", logsHandler)
	http.HandleFunc("/health", healthCheckHandler)

	listenAddr := fmt.Sprintf(":%s", appPort)
	server := &http.Server{
		Addr: listenAddr,
	}

	go func() {
		log.Printf("Starting logging-service HTTP server on %s", listenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error starting HTTP server: %v", err)
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

	ctxHzShutdown, cancelHzShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelHzShutdown()
	if err := hzClient.Shutdown(ctxHzShutdown); err != nil {
		log.Printf("Error shutting down Hazelcast client: %v", err)
	} else {
		log.Println("Hazelcast client shut down successfully.")
	}

	ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(ctxShutdown); err != nil {
		log.Fatalf("HTTP server shutdown failed: %v", err)
	}
	log.Println("Logging Service finished.")
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

func logHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
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

	logMessageJSON, err := json.Marshal(data)
	if err != nil {
		log.Printf("Error marshaling LogMessage to JSON: %v\n", err)
		http.Error(w, "Internal server error: Failed to serialize message", http.StatusInternalServerError)
		return
	}

	_, err = messagesMap.PutIfAbsent(ctx, data.ID, string(logMessageJSON))
	if err != nil {
		log.Printf("Error putting message to Hazelcast: %v\n", err)
		http.Error(w, "Failed to log message to distributed map", http.StatusInternalServerError)
		return
	}

	exists, err := messagesMap.ContainsKey(ctx, data.ID)
	if err != nil {
		log.Printf("Error checking existence of key %s in Hazelcast: %v\n", data.ID, err)
		http.Error(w, "Failed to verify message existence", http.StatusInternalServerError)
		return
	}

	if exists {
		log.Printf("Received message: ID=%s, Msg=%s. Already exists or successfully added to map.\n", data.ID, data.Msg)
	} else {
		log.Printf("Received and saved new message: ID=%s, Msg=%s\n", data.ID, data.Msg)
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
		log.Printf("Error getting messages from Hazelcast: %v\n", err)
		http.Error(w, "Failed to retrieve messages from distributed map", http.StatusInternalServerError)
		return
	}

	var allLogMessages []LogMessage
	for _, entry := range entryValues {
		if msgJSON, ok := entry.Value.(string); ok {
			var msg LogMessage
			if unmarshalErr := json.Unmarshal([]byte(msgJSON), &msg); unmarshalErr != nil {
				log.Printf("Warning: Could not unmarshal log entry from Hazelcast (key: %v): %v (raw: '%s')\n", entry.Key, unmarshalErr, msgJSON)
				continue
			}
			allLogMessages = append(allLogMessages, msg)
		} else {
			log.Printf("Warning: Non-string value found in map for key %v (type %T)\n", entry.Key, entry.Value)
		}
	}

	responseJSON, err := json.Marshal(allLogMessages)
	if err != nil {
		log.Printf("Error marshaling all LogMessages to JSON: %v\n", err)
		http.Error(w, "Internal server error: Failed to serialize messages", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, string(responseJSON))
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "UP"})
}
