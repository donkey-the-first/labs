package main

import (
    "bytes"
    "fmt"
    "io/ioutil"
    "net/http"
    "time"
    "github.com/google/uuid"
)

const maxRetries = 3
const retryDelay = time.Second

func main() {
	// Реєстрація обробників
	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/message", messageHandler)
	http.HandleFunc("/messages", messagesHandler)

	// Запуск сервера на порту 8080
	fmt.Println("Starting facade-service on port 8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}

// Обробник для кореневого шляху "/"
func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Повертаємо інформацію про доступні ендпоінти
	response := `Доступні ендпоінти:
- POST /message: Надіслати повідомлення
- GET /messages: Отримати всі повідомлення
`
	fmt.Fprintf(w, response)
}

// Обробник для POST-запиту /message
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
	//id := "123-123" //Testing deduplications
    payload := fmt.Sprintf(`{"id": "%s", "msg": "%s"}`, id, msg)

    var resp *http.Response
    for attempt := 1; attempt <= maxRetries; attempt++ {
        fmt.Printf("Attempt %d: Sending POST request to logging-service with payload: %s\n", attempt, payload)
        resp, err = http.Post("http://localhost:8081/log", "application/json", bytes.NewBuffer([]byte(payload)))
        if err == nil && resp.StatusCode == http.StatusOK {
            fmt.Printf("Successfully sent message on attempt %d\n", attempt)
            break
        }
        if err != nil {
            fmt.Printf("Error on attempt %d: %v\n", attempt, err)
        } else {
            fmt.Printf("logging-service returned non-OK status on attempt %d: %d\n", attempt, resp.StatusCode)
        }
        if attempt < maxRetries {
            time.Sleep(retryDelay)
        }
    }

    if err != nil || resp.StatusCode != http.StatusOK {
        http.Error(w, "Failed to send message after retries", http.StatusInternalServerError)
        return
    }
    defer resp.Body.Close()

    fmt.Fprintf(w, "Message sent with ID: %s", id)
}

// Обробник для GET-запиту /messages
func messagesHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
        return
    }

    // Логування запиту до logging-service
    fmt.Println("Sending GET request to logging-service at http://localhost:8081/logs")

    logResp, err := http.Get("http://localhost:8081/logs")
    if err != nil {
        fmt.Printf("Error sending GET request to logging-service: %v\n", err)
        http.Error(w, "Error getting logs", http.StatusInternalServerError)
        return
    }
    defer logResp.Body.Close()

    // Логування відповіді від logging-service
    fmt.Printf("Received response from logging-service: %d\n", logResp.StatusCode)

    logs, err := ioutil.ReadAll(logResp.Body)
    if err != nil {
        fmt.Printf("Error reading response from logging-service: %v\n", err)
        http.Error(w, "Error reading logs", http.StatusInternalServerError)
        return
    }

    // Логування запиту до messages-service
    fmt.Println("Sending GET request to messages-service at http://localhost:8082/message")

    msgResp, err := http.Get("http://localhost:8082/message")
    if err != nil {
        fmt.Printf("Error sending GET request to messages-service: %v\n", err)
        http.Error(w, "Error getting message", http.StatusInternalServerError)
        return
    }
    defer msgResp.Body.Close()

    // Логування відповіді від messages-service
    fmt.Printf("Received response from messages-service: %d\n", msgResp.StatusCode)

    message, err := ioutil.ReadAll(msgResp.Body)
    if err != nil {
        fmt.Printf("Error reading response from messages-service: %v\n", err)
        http.Error(w, "Error reading message", http.StatusInternalServerError)
        return
    }

    response := string(logs) + "\n" + string(message)
    fmt.Fprintf(w, response)
}