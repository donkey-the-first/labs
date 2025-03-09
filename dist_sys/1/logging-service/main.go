package main

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
    "net/http"
    "strings" // Додано пакет strings
    "sync"
)

// Хеш-таблиця для зберігання повідомлень
var (
    messages = make(map[string]string)
    mu       sync.Mutex // Для синхронізації доступу до мапи
)

func main() {
	// Реєстрація обробників для POST і GET запитів
	http.HandleFunc("/log", logHandler)
	http.HandleFunc("/logs", logsHandler)

	// Запуск сервера на порту 8081
	fmt.Println("Starting logging-service on port 8081")
	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}

// Обробник POST-запиту
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

    var data map[string]string
    err = json.Unmarshal(body, &data)
    if err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }

    id, ok1 := data["id"]
    msg, ok2 := data["msg"]
    if !ok1 || !ok2 {
        http.Error(w, "Missing id or msg", http.StatusBadRequest)
        return
    }

    mu.Lock()
    if _, exists := messages[id]; exists {
        fmt.Printf("Message with ID %s already exists, skipping\n", id)
    } else {
        messages[id] = msg
        fmt.Printf("Received and saved message: %s with ID: %s\n", msg, id)
    }
    mu.Unlock()

    w.WriteHeader(http.StatusOK)
    fmt.Fprintf(w, "Message logged successfully")
}

// Обробник GET-запиту
func logsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Отримання всіх повідомлень
	mu.Lock()
	var allMessages []string
	for _, msg := range messages {
		allMessages = append(allMessages, msg)
	}
	mu.Unlock()

	// Повернення повідомлень у вигляді рядка
	response := strings.Join(allMessages, "\n")
	fmt.Fprintf(w, response)
}