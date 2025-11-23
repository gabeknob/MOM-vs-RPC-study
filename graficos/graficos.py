import matplotlib.pyplot as plt

workers = ["1", "2", "3", "4", "5"]
avg_latency_test1 = [16.27, 5.21, 4.64, 4.40 , 4.31]
avg_latency_test2=  [ 0, 38.34, 16.12, 8.42, 5.83]
avg_latency_test3 = [ 0, 0, 50.92, 13.41, 9.97]
avg_latency_test4 = [ 0, 0, 0, 137.10, 22.90]
avg_latency_test5 = [ 0, 0, 0, 0, 67.53]

rpsResults  = [135.87 , 361 , 474 , 564, 614]

datasets = {
    "RPS": rpsResults,
    "avg Latency Test 1": avg_latency_test1,
    "avg Latency Test 2": avg_latency_test2,
    "avg Latency Test 3": avg_latency_test3,
    "avg Latency Test 4": avg_latency_test4,
    "avg Latency Test 5": avg_latency_test5,
}

for name, data in datasets.items():
    plt.figure(figsize=(8,5))
    plt.bar(workers, data)
    plt.xlabel("Workers")
    plt.ylabel("ms")
    plt.title(f"{name} vs Workers")
    plt.grid(axis="y")
    plt.savefig(f"{name}")
    #plt.show()
