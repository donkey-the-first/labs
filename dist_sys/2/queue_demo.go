package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/hazelcast/hazelcast-go-client"
)

func main() {
	ctx := context.Background()
	var wg sync.WaitGroup
	numReaders := 0
	producerID := 0
	queueName := "default"
	done := make(chan struct{}) // Канал для сигналу завершення виробника

	// Конфігурація клієнта Hazelcast
	config := hazelcast.NewConfig()
	config.Cluster.Name = "dev-map"
	config.Cluster.Network.SetAddresses("127.0.0.1:5701")

	client, err := hazelcast.StartNewClientWithConfig(ctx, config)
	if err != nil {
		log.Fatalf("Помилка запуску клієнта Hazelcast: %v", err)
	}
	defer client.Shutdown(ctx)

	// Отримання розподіленої черги
	hzQueue, err := client.GetQueue(ctx, queueName)
	if err != nil {
		log.Fatalf("Помилка отримання черги '%s': %v", queueName, err)
	}
	fmt.Printf("Черга '%s' успішно отримана.\n", queueName) // Виправлено: queueName

	// Запуск клієнта-виробника
	wg.Add(1)
	go func(producerID int) {
		defer wg.Done()
		for i := 1; i <= 100; i++ {
			value := strconv.Itoa(i)
			_, err := hzQueue.Add(ctx, value)
			if err != nil {
				log.Printf("Виробник %d: Помилка додавання '%s' до черги: %v", producerID, value, err)
			} else {
				fmt.Printf("Виробник %d: Додано '%s' до черги.\n", producerID, value)
			}
			time.Sleep(time.Millisecond * 50)
		}
		fmt.Printf("Виробник %d завершив додавання 100 елементів.\n", producerID)
		close(done) // Сигналізуємо, що виробник завершив роботу
	}(producerID)

	// Запуск клієнтів-споживачів
	for i := 1; i <= numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for {
				select {
				case <-done: // Якщо виробник завершив роботу
					// Перевіряємо, чи черга порожня
					for {
						item, err := hzQueue.Poll(ctx)
						if err != nil {
							log.Printf("Споживач %d: Помилка отримання елемента з черги: %v", readerID, err)
							time.Sleep(time.Second)
							continue
						}
						if item == nil {
							fmt.Printf("Споживач %d: Черга порожня, завершую роботу.\n", readerID)
							return
						}
						fmt.Printf("Споживач %d: Отримано '%s' з черги.\n", readerID, item)
						time.Sleep(time.Millisecond * 100)
					}
				default:
					item, err := hzQueue.Poll(ctx)
					if err != nil {
						log.Printf("Споживач %d: Помилка отримання елемента з черги: %v", readerID, err)
						time.Sleep(time.Second)
						continue
					}
					if item == nil {
						time.Sleep(time.Millisecond * 100) // Чекаємо, якщо черга порожня
						continue
					}
					fmt.Printf("Споживач %d: Отримано '%s' з черги.\n", readerID, item)
					time.Sleep(time.Millisecond * 100)
				}
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("Всі клієнти завершили роботу з '%s'.\n", queueName)

	// Перевірка поведінки при заповненій черзі
	fmt.Println("\nДемонстрація поведінки при заповненій черзі 'default' без читачів...")
	hzFullQueue, err := client.GetQueue(ctx, "default")
	if err != nil {
		log.Fatalf("Помилка отримання черги 'default': %v", err)
	}

	for i := 1; i <=12; i++ {
		value := fmt.Sprintf("item-%d", i)
		_, err := hzFullQueue.Add(ctx, value)
		if err != nil {
			log.Printf("Спроба додавання '%s' до заповненої черги 'default': %v", value, err)
		} else {
			fmt.Printf("Додано '%s' до черги 'default'.\n", value)
		}
		time.Sleep(time.Millisecond * 200)
	}

	client.Shutdown(ctx)
}