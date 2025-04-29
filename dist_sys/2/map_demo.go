package main

import (
        "context"
        "fmt"
        "log"
        "time"

        "github.com/hazelcast/hazelcast-go-client"
)

func main() {
        // Ініціалізація клієнта Hazelcast
        config := hazelcast.NewConfig()
        config.Cluster.Name = "dev-map"
        config.Cluster.Network.SetAddresses("172.18.0.1:5701")

        // Запуск клієнта
        ctx := context.Background()
        client, err := hazelcast.StartNewClientWithConfig(ctx, config)
        if err != nil {
                log.Fatalf("Помилка запуску клієнта Hazelcast: %v", err)
        }
        defer client.Shutdown(ctx)

        // Отримання або створення розподіленої карти
        hzMap, _ := client.GetMap(ctx, "demo-distributed-map")

        // Очищення карти
        hzMap.Clear(ctx)

        // Вимірювання часу запису
        startTime := time.Now()

        // Запис 1000 значень у карту
        for i := 0; i < 1000; i++ {
                hzMap.Set(ctx, i, i)
        }

        endTime := time.Now()
        duration := endTime.Sub(startTime)

        fmt.Printf("Запис 1000 ключів зайняв: %v\n", duration)

        fmt.Println("Успішно збережено 1000 ключів у Distributed Map")

        // Виведення розміру карти
        size, _ := hzMap.Size(ctx)
        fmt.Printf("Розмір карти: %d\n", size)

        // Виведення перших 5 значень
        for i := 0; i < 5; i++ {
                value, _ := hzMap.Get(ctx, i)
                fmt.Printf("Ключ: %d, Значення: %v\n", i, value)
        }
}