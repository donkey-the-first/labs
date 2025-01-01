import threading
import time
from pymongo import MongoClient
from pymongo import WriteConcern

# Підключення до MongoDB
client = MongoClient('mongodb://mongo1:27017,mongo2:27018,mongo3:27019/?replicaSet=rs0')
db = client.test
collection = db.users

# Ініціалізація даних
increment_value = 10000
num_threads = 10
user_name = "User1"

# Ініціалізуємо колекцію (якщо запису немає, створюємо його)
if not collection.find_one({"user_name": user_name}):
    collection.insert_one({"user_name": user_name, "likes_count": 0})

# Функція для інкрементації каунтера з WriteConcern = majority
def increment_likes_with_writeConcern(user_name, increment_value, write_concern_level):
    write_concern = WriteConcern(w=write_concern_level)
    for _ in range(increment_value):
        collection.with_options(write_concern=write_concern).find_one_and_update(
            {"user_name": user_name},
            {"$inc": {"likes_count": 1}},
            return_document=True
        )

# Скидання каунтера перед запуском
collection.find_one_and_update(
    {"user_name": user_name},
    {"$set": {"likes_count": 0}},
    upsert=False
)

print("Starting...")
threads = []
start_time = time.time()

# Запуск потоків
for _ in range(num_threads):
    thread = threading.Thread(target=increment_likes_with_writeConcern, args=(user_name, increment_value, "majority"))
    threads.append(thread)
    thread.start()

for thread in threads:
    thread.join()

end_time = time.time()
execution_time = end_time - start_time

# Перевірка результату
user = collection.find_one({"user_name": user_name})
expected_likes = num_threads * increment_value
actual_likes = user["likes_count"]
print(f"Time: {execution_time:.2f}")
print(f"Expected: {expected_likes}")
print(f"Real: {actual_likes}")

# Скидання каунтера після тесту
collection.find_one_and_update(
    {"user_name": user_name},
    {"$set": {"likes_count": 0}},
    upsert=False
)
