package main

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	pb "simulation/pkg/protocol"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type LoadBalancerServer struct {
	pb.UnimplementedSimulationServiceServer
	clients map[string]pb.SimulationServiceClient

	workerUrls []string
	mu         sync.Mutex
	counter    int
}

func (s *LoadBalancerServer) GetSalesMetrics(ctx context.Context, req *pb.SalesRequest) (*pb.SalesResponse, error) {
	s.mu.Lock()
	workerUrl := s.workerUrls[s.counter%len(s.workerUrls)]
	s.counter++
	s.mu.Unlock()

	log.Printf("LB: Forwarding request to Worker at %s", workerUrl)

	workerClient := s.clients[workerUrl]

	return workerClient.GetSalesMetrics(ctx, req)
}

func main() {
	workerUrls := []string{"localhost:50052", "localhost:50053"}

	lb := &LoadBalancerServer{
		clients:    make(map[string]pb.SimulationServiceClient),
		workerUrls: workerUrls,
	}

	for _, url := range workerUrls {
		conn, err := grpc.NewClient(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Printf("Failed to connect to worker %s: %v", url, err)
			continue
		}
		lb.clients[url] = pb.NewSimulationServiceClient(conn)
	}

	go func() {
		ticker := time.NewTicker(2 * time.Second)

		for range ticker.C {
			log.Println("------ Health Check ------")
			for url, client := range lb.clients {
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				stats, err := client.GetSystemStats(ctx, &pb.Empty{})

				if err != nil {
					log.Printf("Worker [%s] is DOWN: %v", url, err)
				} else {
					log.Printf("Worker [%s]: RAM=%d MB, Routines=%d",
						url,
						stats.MemoryUsage/1024/1024, // Convert bytes to MB
						stats.ActiveGoroutines,
					)
				}

				cancel()
			}
			log.Println("--------------------------")
		}
	}()

	listener, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()

	pb.RegisterSimulationServiceServer(grpcServer, lb)

	log.Println("RPC Load Balancer running on :50051...")
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
