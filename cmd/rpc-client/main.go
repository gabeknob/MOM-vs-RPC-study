package main

import (
	"context"
	"log"
	"time"

	pb "simulation/pkg/protocol"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func int32ptr(n int32) *int32 {
	return &n
}

func main() {
	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Did not connect: %v", err)
	}

	defer func(conn *grpc.ClientConn) {
		err := conn.Close()
		if err != nil {
			log.Fatalf("Could not close connection: %v", err)
		}
	}(conn)

	client := pb.NewSimulationServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Println("Sending request...")
	res, err := client.GetSalesMetrics(ctx, &pb.SalesRequest{
		Region:   "Europe",
		Category: "Computers",
		StartDate: time.
			Date(2022, 0, 21, 0, 0, 0, 0, time.UTC).
			Format("2006-01-02"),
		StartHour: int32ptr(0),
		EndHour:   int32ptr(9),
	})
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	log.Printf("Response: %s (Count: %d)", res.RegionProcessed, res.TotalSalesCount)
}
