package main

import (
	"fmt"
	"net/http"
)

func main() {
	// Реєстрація обробника для ендпоінта "/message"
	http.HandleFunc("/message", messageHandler)

	// Запуск сервера на порту 8082
	fmt.Println("Starting messages-service on port 8082")
	err := http.ListenAndServe(":8082", nil)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}

// Обробник GET-запиту
func messageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Повернення статичного повідомлення
	fmt.Fprintf(w, "not implemented yet")
}