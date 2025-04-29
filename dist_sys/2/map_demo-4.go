package main

import (
    "context"
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/hazelcast/hazelcast-go-client"
)

func main() {
    ctx := context.Background()
    var wg sync.WaitGroup
    numClients := 3
    iterations := 10000
    errCh := make(chan error, numClients)
    map_name := "demo-distributed-map-4"

    config := hazelcast.NewConfig()
    config.Cluster.Name = "dev-map"
    config.Cluster.Network.SetAddresses("172.18.0.1:5701", "172.18.0.1:5702", "172.18.0.1:5703")
    client, err := hazelcast.StartNewClientWithConfig(ctx, config)
    if err != nil {
        log.Fatalf("Помилка запуску клієнта Hazelcast: %v", err)
    }
    defer client.Shutdown(ctx)

    hzMap, err := client.GetMap(ctx, map_name)
    if err != nil {
        log.Fatalf("Помилка отримання карти: %v", err)
    }

    hzMap.Clear(ctx)
    _, err = hzMap.PutIfAbsent(ctx, "key", int64(0))
    if err != nil {
        log.Fatalf("Помилка ініціалізації ключа 'key': %v", err)
    }

    startTime := time.Now()

    for i := 0; i < numClients; i++ {
        wg.Add(1)
        go func(clientID int) {
            defer wg.Done()
            client, err := hazelcast.StartNewClientWithConfig(ctx, config)
            if err != nil {
                errCh <- fmt.Errorf("клієнт %d: помилка запуску клієнта: %v", clientID, err)
                return
            }
            defer client.Shutdown(ctx)

            hzMap, err := client.GetMap(ctx, map_name)
            if err != nil {
                errCh <- fmt.Errorf("клієнт %d: помилка отримання карти: %v", clientID, err)
                return
            }

            for k := 0; k < iterations; k++ {
                for {
                    value, err := hzMap.Get(ctx, "key")
                    if err != nil {
                        log.Printf("Клієнт %d: Помилка отримання значення: %v", clientID, err)
                        break
                    }
                    val, ok := value.(int64)
                    if !ok {
                        log.Printf("Клієнт %d: Некоректний тип значення: %v", clientID, value)
                        break
                    }
                    newVal := val + 1
                    replaced, err := hzMap.ReplaceIfSame(ctx, "key", val, newVal)
                    if err != nil {
                        log.Printf("Клієнт %d: Помилка заміни значення: %v", clientID, err)
                        break
                    }
                    if replaced {
                        break // Успішно оновлено
                    }
                }
            }
            fmt.Printf("Клієнт %d завершив %d ітерацій\n", clientID, iterations)
        }(i)
    }

    wg.Wait()
    close(errCh)

    for err := range errCh {
        log.Println(err)
    }

    endTime := time.Now()
    duration := endTime.Sub(startTime)

    finalValue, err := hzMap.Get(ctx, "key")
    if err != nil {
        log.Fatalf("Помилка отримання кінцевого значення: %v", err)
    }

    fmt.Printf("Кінцеве значення ключа 'key': %v\n", finalValue)
    fmt.Printf("Очікуване значення: %d\n", numClients*iterations)
    fmt.Printf("Час виконання: %v\n", duration)
}