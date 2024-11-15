import hazelcast
from hazelcast.config import Config
import concurrent.futures
import time

def inc_atomic_long(counter, num_iterations):
    for _ in range(num_iterations):
        counter.increment_and_get()

def main():
    print("Connecting...")
    config = Config()
    config.cluster_members = ['localhost:5704', 'localhost:5705', 'localhost:5706']
    config.cluster_name = "test"

    client = hazelcast.HazelcastClient(config)
    print("Connected :-)")
    
    counter_name = 'counter'
    counter = client.cp_subsystem.get_atomic_long(counter_name).blocking()
    
    num_threads = 10
    num_iterations_per_thread = 1000
    total_expected = num_threads * num_iterations_per_thread
    
    counter.set(0)
    print(f"Start value: {counter.get()}")
    
    print("--==  started  ==--")
    start_time = time.time()
    
    with concurrent.futures.ThreadPoolExecutor(max_workers=num_threads) as executor:
        futures = [
            executor.submit(inc_atomic_long, counter, num_iterations_per_thread)
            for _ in range(num_threads)
        ]
        concurrent.futures.wait(futures)
    
    end_time = time.time()
    print("--==  ended  ==--")
    
    final_value = counter.get()
    print(f"End value: {final_value} (expected {total_expected}) {'Equal' if final_value == total_expected else 'Not equal'}")
    print(f"Done in: {round(end_time - start_time, 3)} sec ({round((end_time - start_time)/60, 3)} min)")
    
    client.shutdown()

if __name__ == "__main__":
    main()
