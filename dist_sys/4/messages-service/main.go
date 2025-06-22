package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/segmentio/kafka-go"
)

const (
	configServiceURL = "http://localhost:7201/messages-service"
	kafkaTopic       = "messages"
	consumerGroupID  = "messages-service-group"
)

type ConfigServiceResponse struct {
	Addresses    []string `json:"addresses"`
	KafkaBrokers []string `json:"kafka_brokers"`
}

type Message struct {
	ID  string `json:"id"`
	Msg string `json:"msg"`
}

var (
	messages []Message
	port     string
)

func main() {
	servicePort := flag.Int("port", 0, "Port for the messages service to listen on")
	flag.Parse()

	config, err := getConfig()
	if err != nil {
		log.Fatalf("Failed to get configuration: %v", err)
	}

	if *servicePort != 0 {
		port = fmt.Sprintf("%d", *servicePort)
	} else if len(config.Addresses) > 0 {
		port = config.Addresses[0]
	} else {
		port = "8081"
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  config.KafkaBrokers,
		Topic:    kafkaTopic,
		GroupID:  consumerGroupID,
		MinBytes: 10e3,
		MaxBytes: 10e6,
	})
	defer reader.Close()

	go func() {
		for {
			msg, err := reader.ReadMessage(context.Background())
			if err != nil {
				log.Printf("Error reading from Kafka: %v", err)
				continue
			}
			var message Message
			if err := json.Unmarshal(msg.Value, &message); err != nil {
				log.Printf("Error unmarshaling message: %v", err)
				continue
			}
			messages = append(messages, message)
			log.Printf("Received message: ID=%s, Msg=%s", message.ID, message.Msg)
		}
	}()

	http.HandleFunc("/message", messageHandler)
	http.HandleFunc("/messages", messagesHandler)
	log.Printf("Starting messages-service on port %s", port)
	err = http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}

func getConfig() (ConfigServiceResponse, error) {
	resp, err := http.Get(configServiceURL)
	if err != nil {
		return ConfigServiceResponse{}, fmt.Errorf("error getting config from config-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ConfigServiceResponse{}, fmt.Errorf("config-service returned non-OK status: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return ConfigServiceResponse{}, fmt.Errorf("error reading config-service response body: %w", err)
	}

	var config ConfigServiceResponse
	err = json.Unmarshal(body, &config)
	if err != nil {
		return ConfigServiceResponse{}, fmt.Errorf("error unmarshaling config-service response: %w", err)
	}

	if len(config.KafkaBrokers) == 0 {
		config.KafkaBrokers = []string{"kafka-01:8092", "kafka-02:8092"}
	}

	return config, nil
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

	config, err := getConfig()
	if err != nil {
		log.Printf("Failed to get config: %v", err)
		http.Error(w, "Failed to get Kafka configuration", http.StatusInternalServerError)
		return
	}

	writer := &kafka.Writer{
		Addr:                   kafka.TCP(config.KafkaBrokers...),
		Topic:                  kafkaTopic,
		Balancer:               &kafka.LeastBytes{},
		RequiredAcks:           kafka.RequireAll,
		AllowAutoTopicCreation: true,
	}
	defer writer.Close()

	err = writer.WriteMessages(context.Background(), kafka.Message{Value: body})
	if err != nil {
		log.Printf("Error writing to Kafka: %v", err)
		http.Error(w, fmt.Sprintf("Error writing to Kafka: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Message written to Kafka")
	fmt.Fprintf(w, "Message written to Kafka")
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

	fmt.Fprintf(w, string(response))
}
