package main

import (
	"context"
	"log"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	pb "simulation/pkg/protocol"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func int32ptr(n int32) *int32 {
	return &n
}

func main() {
	targetRate := 100
	duration := 30 * time.Second
	concurrency := 15

	var successCount int64
	var failCount int64
	var totalLatency int64

	conns := make([]*grpc.ClientConn, concurrency)
	clients := make([]pb.SimulationServiceClient, concurrency)

	log.Println("Connecting clients...")
	for i := 0; i < concurrency; i++ {
		conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("Did not connect: %v", err)
		}
		conns[i] = conn
		clients[i] = pb.NewSimulationServiceClient(conn)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		defer wg.Done()

		interval := time.Second / time.Duration(targetRate)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		timer := time.NewTimer(duration)
		clientIdx := 0

		for {
			select {
			case <-timer.C:
				return // Stop after duration
			case <-ticker.C:
				go func(client pb.SimulationServiceClient) {
					start := time.Now()
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()

					//region := db.RegionNames[rand.IntN(len(db.RegionNames))]
					//category := db.ProductCategories[rand.IntN(len(db.ProductCategories))]
					rangeDateStart := time.Now().Add(time.Hour * -time.Duration(rand.IntN(365*5)*24))
					rangeDateEnd := time.Now().Add(time.Hour * -time.Duration(rand.IntN(int(time.Now().Sub(rangeDateStart).Hours()+1))))
					hourRangeStart := rand.IntN(23)
					hourRangeEnd := rand.IntN(23-hourRangeStart+1) + hourRangeStart + 1

					res, err := client.GetSalesMetrics(ctx, &pb.SalesRequest{
						//Region:    region,
						Region:   "Europe",
						Category: "Computers",
						//Category:  category,
						StartDate: rangeDateStart.Format("2006-01-02"),
						EndDate:   rangeDateEnd.Format("2006-01-02"),
						StartHour: int32ptr(int32(hourRangeStart)),
						EndHour:   int32ptr(int32(hourRangeEnd)),
					})
					elapsed := time.Since(start).Milliseconds()

					if err != nil {
						atomic.AddInt64(&failCount, 1)
						log.Printf("Error: %v", err)
					} else {
						atomic.AddInt64(&successCount, 1)
						atomic.AddInt64(&totalLatency, elapsed)

						log.Printf("Response: %s (Count: %d) in %d",
							res.RegionProcessed,
							res.TotalSalesCount,
							elapsed,
						)
					}
				}(clients[clientIdx%concurrency])

				clientIdx++
			}
		}
	}()

	wg.Wait()

	avgLatency := 0.0
	if successCount > 0 {
		avgLatency = float64(totalLatency) / float64(successCount)
	}

	log.Println("--- 📊 RESULTS ---")
	log.Printf("Total Requests: %d", successCount+failCount)
	log.Printf("Success: %d", successCount)
	log.Printf("Failed: %d", failCount)
	log.Printf("Avg Latency: %.2f ms", avgLatency)
	log.Printf("Actual RPS: %.2f", float64(successCount)/duration.Seconds())
}
