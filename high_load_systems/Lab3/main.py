from neo4j import GraphDatabase
from neo4j.exceptions import TransientError
import time
import threading
import os
from dotenv import load_dotenv

load_dotenv()
uri = "bolt://localhost:7687"
username = os.getenv("USER")
password = os.getenv("PASSWORD")
driver = GraphDatabase.driver(uri, auth=(username, password))


# Функції для роботи з базою даних
def create_customer(tx, name, email):
    tx.run("""
        MERGE (c:Customer {email: $email})
        ON CREATE SET c.name = $name
        """, name=name, email=email)

def create_item(tx, name, price, description):
    tx.run("""
        MERGE (i:Item {name: $name})
        ON CREATE SET i.price = $price, i.description = $description
        """, name=name, price=price, description=description)

def create_order(tx, order_id, order_date, status):
    tx.run("""
        MERGE (o:Order {orderId: $order_id})
        ON CREATE SET o.orderDate = $order_date, o.status = $status
        """, order_id=order_id, order_date=order_date, status=status)

def link_customer_order(tx, email, order_id):
    tx.run("""
        MATCH (c:Customer {email: $email})
        MATCH (o:Order {orderId: $order_id})
        MERGE (c)-[r:PLACED]->(o)
        """, email=email, order_id=order_id)

def link_order_item(tx, order_id, item_name):
    tx.run("""
        MATCH (o:Order {orderId: $order_id})
        MATCH (i:Item {name: $item_name})
        MERGE (o)-[r:CONTAINS]->(i)
        """, order_id=order_id, item_name=item_name)

def create_viewed_relationship(tx, email, item_name):
    tx.run("""
        MATCH (c:Customer {email: $email})
        MATCH (i:Item {name: $item_name})
        MERGE (c)-[r:VIEWED]->(i)
        ON CREATE SET r.viewDate = datetime()
        """, email=email, item_name=item_name)

# Основний код
with driver.session() as session:
    # Створення клієнта
    session.execute_write(create_customer, "Іван Петренко", "ivan.petrenko@example.com")

    # Створення товарів
    session.execute_write(create_item, "Ноутбук", 1500, "Ноутбук з 16ГБ RAM")
    session.execute_write(create_item, "Смартфон", 800, "Смартфон з 128ГБ пам'яті")
    session.execute_write(create_item, "Планшет", 600, "Планшет з 64ГБ пам'яті")

    # Створення замовлення
    session.execute_write(create_order, "ORD123", "2023-10-01", "Completed")

    # Зв'язок клієнта з замовленням
    session.execute_write(link_customer_order, "ivan.petrenko@example.com", "ORD123")

    # Додавання товарів до замовлення
    session.execute_write(link_order_item, "ORD123", "Ноутбук")
    session.execute_write(link_order_item, "ORD123", "Смартфон")

    # Клієнт переглянув товар без покупки
    session.execute_write(create_viewed_relationship, "ivan.petrenko@example.com", "Планшет")

print("1. Знайти Items які входять в конкретний Order")

def get_items_in_order(tx, order_id):
    result = tx.run("""
        MATCH (o:Order {orderId: $orderId})-[:CONTAINS]->(i:Item)
        RETURN i.name AS itemName, i.price AS price, i.description AS description
        """, orderId=order_id)
    return result.data()

with driver.session() as session:
    items = session.execute_read(get_items_in_order, "ORD123")
    print(items)


print("2. Підрахувати вартість конкретного Order")

def calculate_order_total(tx, order_id):
    result = tx.run("""
        MATCH (o:Order {orderId: $orderId})-[:CONTAINS]->(i:Item)
        RETURN SUM(i.price) AS totalCost
        """, orderId=order_id)
    return result.single()["totalCost"]

with driver.session() as session:
    total_cost = session.execute_read(calculate_order_total, "ORD123")
    print(f"Total cost of order: {total_cost}")

print("3. Знайти всі Orders конкретного Customer")

def get_customer_orders(tx, email):
    result = tx.run("""
        MATCH (c:Customer {email: $email})-[:PLACED]->(o:Order)
        RETURN o.orderId AS orderId, o.orderDate AS orderDate, o.status AS status
        """, email=email)
    return result.data()

with driver.session() as session:
    orders = session.execute_read(get_customer_orders, "ivan.petrenko@example.com")
    print(orders)

print("4. Знайти всі Items куплені конкретним Customer (через Order)")

def get_items_purchased_by_customer(tx, email):
    result = tx.run("""
        MATCH (c:Customer {email: $email})-[:PLACED]->(:Order)-[:CONTAINS]->(i:Item)
        RETURN DISTINCT i.name AS itemName, i.price AS price, i.description AS description
        """, email=email)
    return result.data()

with driver.session() as session:
    items = session.execute_read(get_items_purchased_by_customer, "ivan.petrenko@example.com")
    print(items)

print("5. Знайти кількість Items куплені конкретним Customer (через Order)")

def count_items_purchased_by_customer(tx, email):
    result = tx.run("""
        MATCH (c:Customer {email: $email})-[:PLACED]->(:Order)-[:CONTAINS]->(i:Item)
        RETURN COUNT(i) AS itemCount
        """, email=email)
    return result.single()["itemCount"]

with driver.session() as session:
    item_count = session.execute_read(count_items_purchased_by_customer, "ivan.petrenko@example.com")
    print(f"Items purchased by customer: {item_count}")

print("6. Знайти для Customer на яку суму він придбав товарів (через Order)")

def calculate_total_spent_by_customer(tx, email):
    result = tx.run("""
        MATCH (c:Customer {email: $email})-[:PLACED]->(:Order)-[:CONTAINS]->(i:Item)
        RETURN SUM(i.price) AS totalSpent
        """, email=email)
    return result.single()["totalSpent"]

with driver.session() as session:
    total_spent = session.execute_read(calculate_total_spent_by_customer, "ivan.petrenko@example.com")
    print(f"Total spent by customer: {total_spent}")

print("7. Знайті скільки разів кожен товар був придбаний, відсортувати за цим значенням")

def get_item_purchase_counts(tx):
    result = tx.run("""
        MATCH (:Order)-[:CONTAINS]->(i:Item)
        RETURN i.name AS itemName, COUNT(*) AS purchaseCount
        ORDER BY purchaseCount DESC
        """)
    return result.data()

with driver.session() as session:
    item_counts = session.execute_read(get_item_purchase_counts)
    print(item_counts)

print("8. Знайти всі Items переглянуті (view) конкретним Customer")

def get_items_viewed_by_customer(tx, email):
    result = tx.run("""
        MATCH (c:Customer {email: $email})-[:VIEWED]->(i:Item)
        RETURN i.name AS itemName, i.price AS price, i.description AS description
        """, email=email)
    return result.data()

with driver.session() as session:
    viewed_items = session.execute_read(get_items_viewed_by_customer, "ivan.petrenko@example.com")
    print(viewed_items)

print("9. Знайти інші Items що купувались разом з конкретним Item (тобто всі Items що входять до Order-s разом з даними Item)")

def get_items_purchased_with_item(tx, item_name):
    result = tx.run("""
        MATCH (i:Item {name: $itemName})<-[:CONTAINS]-(o:Order)-[:CONTAINS]->(otherItem:Item)
        WHERE otherItem <> i
        RETURN DISTINCT otherItem.name AS itemName, otherItem.price AS price, otherItem.description AS description
        """, itemName=item_name)
    return result.data()

with driver.session() as session:
    items = session.execute_read(get_items_purchased_with_item, "Ноутбук")
    print(items)

print("10. Знайти Customers які купили даний конкретний Item")

def get_customers_who_purchased_item(tx, item_name):
    result = tx.run("""
        MATCH (c:Customer)-[:PLACED]->(:Order)-[:CONTAINS]->(i:Item {name: $itemName})
        RETURN DISTINCT c.name AS customerName, c.email AS email
        """, itemName=item_name)
    return result.data()

with driver.session() as session:
    customers = session.execute_read(get_customers_who_purchased_item, "Ноутбук")
    print(customers)

print("11. Знайти для певного Customer(а) товари, які він переглядав, але не купив")

def get_viewed_but_not_purchased_items(tx, email):
    result = tx.run("""
        MATCH (c:Customer {email: $email})-[:VIEWED]->(i:Item)
        WHERE NOT (c)-[:PLACED]->(:Order)-[:CONTAINS]->(i)
        RETURN DISTINCT i.name AS itemName, i.price AS price, i.description AS description
        """, email=email)
    return result.data()

with driver.session() as session:
    items = session.execute_read(get_viewed_but_not_purchased_items, "ivan.petrenko@example.com")
    print(items)




#Функція інкременту з повторними спробами
def increment_likes(tx, item_name):
    tx.run("""
        MATCH (i:Item {name: $item_name})
        SET i.likes = i.likes + 1
        """, item_name=item_name)

def safe_increment_likes(session, item_name, increments):
    for _ in range(increments):
        success = False
        retries = 0
        max_retries = 5
        while not success and retries < max_retries:
            try:
                session.execute_write(increment_likes, item_name)
                success = True
            except TransientError as e:
                retries += 1
        if not success:
            print(f"Failed to increment after {max_retries} retries.")

def add_likes_field(tx, item_name):
    tx.run("""
        MATCH (i:Item {name: $item_name})
        SET i.likes = 0
        """, item_name=item_name)

def thread_function(item_name, increments):
    with driver.session() as session:
        safe_increment_likes(session, item_name, increments)

item_name = "Ноутбук"
increments_per_thread = 10000
num_threads = 10

# Додавання поля likes до Item
with driver.session() as session:
    session.execute_write(add_likes_field, item_name)

# Створення та запуск потоків
threads = []
start_time = time.time()
for _ in range(num_threads):
    thread = threading.Thread(target=thread_function, args=(item_name, increments_per_thread))
    threads.append(thread)
    thread.start()

# Очікування завершення всіх потоків
for thread in threads:
    thread.join()
end_time = time.time()

# Отримання фінального значення likes
def get_likes(tx, item_name):
    result = tx.run("""
        MATCH (i:Item {name: $item_name})
        RETURN i.likes AS likes
        """, item_name=item_name)
    return result.single()["likes"]

with driver.session() as session:
    total_likes = session.execute_read(get_likes, item_name)
    print(f"Total likes for '{item_name}': {total_likes}")

print(f"Total execution time: {end_time - start_time:.3f} seconds")