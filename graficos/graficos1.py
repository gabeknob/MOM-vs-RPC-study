import matplotlib.pyplot as plt
import numpy as np

workers = ["1", "2", "3", "4", "5","6","7","8","9","10"]
avg_latency = [10518.16,
               6211.81,
               2304.58,
               87.67,
               6.38,
               5.46,
               5.77,
               5.82,
               5.15,
               5.68
               ]

avg_latency_log = np.log10(avg_latency)

datasets = {
    "avg": avg_latency,
    "avglog": avg_latency_log,
}

for name, data in datasets.items():
    
    plt.figure(figsize=(8,5))
    
    bars = plt.bar(workers, data)

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

        if name=="avglog":
            plt.ylabel("Latência média log 10 (ms)")
            plt.xlabel("número de Workers")
            plt.title(f"Latência média vs Número de workers (log) ")
        else:
            plt.ylabel("Latência média (ms)")
            plt.xlabel("número de Workers")
            plt.title(f"Latência média vs Número de workers")
            


    plt.grid(axis="y")
    plt.savefig(f"{name}.png")   # salva como PNG certinho
    # plt.show()
