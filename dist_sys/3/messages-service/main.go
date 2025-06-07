package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/message", messageHandler)

	fmt.Println("Starting messages-service on port 8082")
	err := http.ListenAndServe(":8082", nil)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}

func messageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	fmt.Fprintf(w, "not implemented yet")
}
