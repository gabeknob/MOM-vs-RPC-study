package main

import (
	"context"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	pb "simulation/pkg/protocol"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type LoadBalancerServer struct {
	pb.UnimplementedSimulationServiceServer
	clients map[string]pb.SimulationServiceClient

	workerUrls    []string
	workerPending map[string]int32
	workerReqs    map[string]uint32
	isHealthy     map[string]bool

	mu      sync.Mutex
	counter int
}

func (s *LoadBalancerServer) GetSalesMetrics(ctx context.Context, req *pb.SalesRequest) (*pb.SalesResponse, error) {
	s.mu.Lock()

	bestWorker := ""
	minPending := int32(999999)

	availableCount := 0
	numWorkers := len(s.workerUrls)

	startIndex := s.counter % numWorkers
	s.counter++ // Increment for next time

	for i := 0; i < numWorkers; i++ {
		idx := (startIndex + i) % numWorkers
		url := s.workerUrls[idx]

		if !s.isHealthy[url] {
			continue
		}
		availableCount++

		pending := s.workerPending[url]

		if pending < minPending {
			minPending = pending
			bestWorker = url
		}
	}

	if availableCount == 0 {
		return nil, status.Errorf(codes.Unavailable, "No healthy workers available to handle request")
	}

	s.workerPending[bestWorker]++
	s.workerReqs[bestWorker]++

	s.mu.Unlock()

	log.Printf("Routing to %s (Pending: %d)", bestWorker, minPending)

	workerClient := s.clients[bestWorker]
	resp, err := workerClient.GetSalesMetrics(ctx, req)

	s.mu.Lock()
	s.workerPending[bestWorker]--
	s.mu.Unlock()

	return resp, err
}

func main() {
	lb := &LoadBalancerServer{
		clients:       make(map[string]pb.SimulationServiceClient),
		workerPending: make(map[string]int32),
		workerUrls:    strings.Split(os.Getenv("WORKER_URLS"), ","),
		workerReqs:    make(map[string]uint32),
		isHealthy:     make(map[string]bool),
	}

	for _, url := range lb.workerUrls {
		conn, err := grpc.NewClient(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Printf("Failed to connect to worker %s: %v", url, err)
			continue
		}
		lb.clients[url] = pb.NewSimulationServiceClient(conn)
	}

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		report := time.NewTicker(10 * time.Second)

		for {
			select {
			case <-ticker.C:
				for url, client := range lb.clients {
					ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
					_, err := client.GetSystemStats(ctx, &pb.Empty{})

					lb.mu.Lock()
					if err != nil {
						if lb.isHealthy[url] {
							log.Printf("Worker [%s] DOWN", url)
						}
						lb.isHealthy[url] = false
					} else {
						if !lb.isHealthy[url] {
							log.Printf("Worker [%s] RECOVERED", url)
						}
						lb.isHealthy[url] = true
					}
					lb.mu.Unlock()
					cancel()
				}
			case <-report.C:
				log.Printf("Worker requests: %v", lb.workerReqs)
			}
		}
	}()

	listener, err := net.Listen("tcp", ":80")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()

	pb.RegisterSimulationServiceServer(grpcServer, lb)

	log.Println("RPC Load Balancer running on :80...")
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
