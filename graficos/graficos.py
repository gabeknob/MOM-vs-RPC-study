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
    "max 135 RPS": avg_latency_test1,
    "max 361 RPS": avg_latency_test2,
    "max 474 RPS": avg_latency_test3,
    "max 564 RPS": avg_latency_test4,
    "max 614 RPS": avg_latency_test5,
}

for name, data in datasets.items():
    
    plt.figure(figsize=(8,5))
    
    bars = plt.bar(workers, data)

    # --- ADICIONAR VALORES ACIMA DAS BARRAS ---
    for bar in bars:
        height = bar.get_height()
        plt.text(
            bar.get_x() + bar.get_width() / 2,
            height,
            f"{height:.2f}",
            ha="center",
            va="bottom",
            fontsize=9
        )
    # ------------------------------------------

    if name != "RPS":
        plt.xlabel("número de Workers")
        plt.ylabel("ms")
        plt.title(f"Latência média vs Número de workers a {name}")
    else:
        plt.xlabel("número de Workers")
        plt.ylabel("número de requests")
        plt.title(f"RPS máximo vs número de Workers")

    plt.grid(axis="y")
    plt.savefig(f"{name}.png")   # salva como PNG certinho
    # plt.show()
