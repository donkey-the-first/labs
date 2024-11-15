import hazelcast
from hazelcast.config import Config
import concurrent.futures
import time
import os

def inc_map_noblock(map, key, num_iterations):
    for _ in range(num_iterations):
        map.put(key, map.get(key) + 1)

def inc_map_pessimistic(map,key, num_iterations):
    for _ in range(num_iterations):
        map.lock(key)
        try:
            map.put(key, map.get(key) + 1)
        finally:
            map.unlock(key)

def inc_map_optimistic(distributed_map, key, num_iterations):
    for _ in range(num_iterations):
        while True:
            old_value = distributed_map.get(key)
            new_value = old_value + 1
            if (distributed_map.replace_if_same(key, old_value ,new_value)):
                break

def main():
    print("connection...")
    config = Config()
    config.cluster_members = ['localhost:5701', 'localhost:5702', 'localhost:5703']  
    client = hazelcast.HazelcastClient(config)
    time.sleep(10)
    print(os.system('sudo docker logs hz-1 | grep -Pzo \'Members {.*} \\[\\n(.*Member.*\\n)+\\]\\n\''))
    print("connected :-)")
    map_name = 'map-1'
    distributed_map = client.get_map(map_name).blocking()
    key = 'counter'
    distributed_map.put(key, 0)
    num_threads = 10
    num_iterations_per_thread = 1000
    print(f"Start value: {distributed_map.get(key)}")

    print("--==  started  ==--")
    start_time = time.time()

    with concurrent.futures.ThreadPoolExecutor(max_workers=num_threads) as executor:
        futures = [
            executor.submit(inc_map_optimistic, distributed_map, key, num_iterations_per_thread)
            for _ in range(num_threads)
        ]
        concurrent.futures.wait(futures)

    end_time = time.time()
    print("--==  ended  ==--")

    final_value = distributed_map.get(key)
    print(f"End value: {final_value} (expected {num_threads * num_iterations_per_thread}) {"Equal" if final_value==num_threads * num_iterations_per_thread else "Not equal"}")
    print(f"Done in: {round(end_time - start_time, 3)} sec ({round((end_time - start_time)/60, 3)}) min")

    client.shutdown()

if __name__ == "__main__":
    main()