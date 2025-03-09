# Мікросервісна архітектура: Базова система з трьома сервісами

Цей проєкт реалізує базову мікросервісну архітектуру з трьома сервісами: `facade-service`, `logging-service` та `messages-service`. Кожен сервіс виконує свою функцію та взаємодіє з іншими через REST API.

## Можливості сервісів

### `facade-service`
- Приймає повідомлення від клієнта через POST-запити, додає унікальний UUID.
- Пересилає повідомлення до `logging-service` та об'єднує дані з `logging-service` і `messages-service` для GET-запитів.
- Ендпоінти:
  - `POST /message` – надіслати повідомлення.
  - `GET /messages` – отримати всі повідомлення.

### `logging-service`
- Зберігає повідомлення з UUID у локальній хеш-таблиці, уникаючи дублювання.
- Ендпоінти:
  - `POST /log` – зберегти повідомлення.
  - `GET /logs` – отримати збережені повідомлення.

### `messages-service`
- Повертає статичне повідомлення "not implemented yet".
- Ендпоінт:
  - `GET /message` – отримати статичне повідомлення.

## Як запустити

1. Запустіть кожен сервіс у окремому терміналі:
   ```bash
   cd facade-service && go run main.go
   cd logging-service && go run main.go
   cd messages-service && go run main.go
   ```

2. Тестуйте ендпоінти через curl або Postman.
- Надіслати повідомлення:
   ```bash
   curl -X POST http://localhost:8080/message -d "Hello, World!"
   ```
   ```
   Message sent with ID: d30485a5-2d73-45f5-8c5b-082a6c10a7c2
   ```
   Отримуємо такий результат в консолі `facade-service`:
   ```
   Starting facade-service on port 8080
   Attempt 1: Sending POST request to logging-service with payload: {"id": "d30485a5-2d73-45f5-8c5b-082a6c10a7c2", "msg": "Hello, World!"}
   Successfully sent message on attempt 1
   ```

- Отримати повідомлення:
   ```bash
   curl -X GET http://localhost:8080/messages
   ```
   ```
   Hello, World!
   not implemented yet
   ```
   Отримуємо такий результат в консолі `facade-service`:
   ```
   Sending GET request to logging-service at http://localhost:8081/logs
   Received response from logging-service: 200
   Sending GET request to messages-service at http://localhost:8082/message
   Received response from messages-service: 200
   ```

## Додаткові функції
- **Retry**: `facade-service` повторює спроби відправки до `logging-service` до 3 разів із затримкою 1 секунда.  
   Для тестування цієї функції сервіс `logging-service` вмикається через секунду після спроби створення повідомлення.
   ```
   curl -X POST http://localhost:8080/message -d "Hello, World!"
   ```
   ```
   Message sent with ID: 1b9789f5-378f-4999-ad07-0c06aafadc93
   ```
   Отримуємо такий результат в консолі `facade-service`:
   ```
   Starting facade-service on port 8080
   Attempt 1: Sending POST request to logging-service with payload: {"id": "1b9789f5-378f-4999-ad07-0c06aafadc93", "msg": "Hello, World!"}
   Error on attempt 1: Post "http://localhost:8081/log": dial tcp 127.0.0.1:8081: connect: connection refused
   Attempt 2: Sending POST request to logging-service with payload: {"id": "1b9789f5-378f-4999-ad07-0c06aafadc93", "msg": "Hello, World!"}
   Error on attempt 2: Post "http://localhost:8081/log": dial tcp 127.0.0.1:8081: connect: connection refused
   Attempt 3: Sending POST request to logging-service with payload: {"id": "1b9789f5-378f-4999-ad07-0c06aafadc93", "msg": "Hello, World!"}
   Successfully sent message on attempt 3
   ```

- **Deduplication**: `logging-service` не зберігає дублікати повідомлень з однаковим UUID.  
   Для перевірки потрібно виконати невеличкі змінення в коді
   ```
   curl -X POST http://localhost:8080/message -d "Hello, World!"
   curl -X POST http://localhost:8080/message -d "Hello, World!"
   ```
   ```
   Message sent with ID: 123-123
   Message sent with ID: 123-123
   ```
   Отримуємо такий результат в консолі `facade-service`:
   ```
   Attempt 1: Sending POST request to logging-service with payload: {"id": "123-123", "msg": "Hello, World!"}
   Successfully sent message on attempt 1
   Attempt 1: Sending POST request to logging-service with payload: {"id": "123-123", "msg": "Hello, World!"}
   Successfully sent message on attempt 1
   ```
   Отримуємо такий результат в консолі `logging-service`:
   ```
   Starting logging-service on port 8081
   Received and saved message: Hello, World! with ID: 123-123
   Message with ID 123-123 already exists, skipping
   ```