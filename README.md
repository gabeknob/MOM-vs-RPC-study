# MOM-vs-RPC-study
A performance comparison between Synchronous (gRPC) and Asynchronous (RabbitMQ) architectures for handling complex database queries in a distributed environment.

📋 Prerequisites

Go 1.24+ installed locally.

Docker & Docker Compose running.

Git.

🛠️ Quick Start

1. Generate the Data (Seeding)

Before running anything, we must generate the mock database (database.db). This script creates Regions, Stores, Products, and 10,000+ Sales records with indexes for performance.

# Run the seeder from the project root
go run cmd/seeder/main.go


Note: If you change the database schema (pkg/db/models.go), you must delete database.db and run this command again.

2. Run the RPC Cluster (gRPC)

We use Docker Compose to spin up the Load Balancer and multiple Workers.

# Build and start the RPC infrastructure
docker compose -f docker-compose.rpc.yml up --build


What starts up?

Load Balancer (:50051): Entry point. Uses "Least Outstanding Requests" strategy.

Workers (:50052): 2+ instances that execute GORM queries on the shared SQLite DB.

3. Run the Benchmark (The Hammer) 🔨

Once the cluster is up (look for "Health Check 🟢" logs in the Docker terminal), open a new terminal to run the stress test.

# Run the concurrent load generator
go run cmd/rpc-client/main.go


Configuration:
You can tweak cmd/rpc-client/main.go to change:

targetRate: Requests per second (Default: 500).

concurrency: Number of parallel connections (Default: 50).

duration: Test length.

🧠 Architecture Overview (RPC)

This project implements a Smart Load Balancer using gRPC.

Client: Simulates 50 concurrent users sending heavy queries (Date Range + Category + Time of Day filters).

Load Balancer:

Routing: Uses "Least Outstanding Requests" (Local Counter). It tracks how many requests are currently pending for each worker and routes to the freest one.

Health Check: Runs a background ticker to ping workers. If a worker dies, it is removed from rotation instantly (Circuit Breaker).

Workers: Receive Proto requests, convert them to SQL via GORM, and return aggregated metrics (Count/Sum).

🐛 Troubleshooting

"Binary was compiled with 'CGO_ENABLED=0'..."

This means the Docker build failed to include GCC. Run docker compose -f docker-compose.rpc.yml build --no-cache to force a clean build with the C compiler.

"Disk is full"

Docker caches layers aggressively. Run docker system prune -a -f to clean up old images.

Performance is slow (< 100 RPS)

Ensure you ran the Seeder recently. The seeder creates database Indexes (gorm:"index"). Without indexes, SQLite performs a full table scan on every request.