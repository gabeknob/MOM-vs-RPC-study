package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"simulation/pkg/utils"

	"google.golang.org/grpc"
	"gorm.io/gorm"

	"simulation/pkg/db"
	pb "simulation/pkg/protocol"
)

type WorkerServer struct {
	pb.UnimplementedSimulationServiceServer
	DB *gorm.DB
}

func (s *WorkerServer) GetSalesMetrics(_ context.Context, req *pb.SalesRequest) (*pb.SalesResponse, error) {
	query := s.DB.Model(&db.Sale{})

	// Filter by Region (Requires joining Store -> Region)
	if req.Region != "" {
		query = query.Joins("JOIN stores ON stores.id = sales.store_id").
			Joins("JOIN regions ON regions.id = stores.region_id").
			Where("regions.name = ?", req.Region)
	}

	// Filter by Category (Requires joining Product)
	if req.Category != "" {
		query = query.Joins("JOIN products ON products.id = sales.product_id").
			Where("products.category = ?", req.Category)
	}

	// Filter by buyer name
	if req.BuyerKeyword != "" {
		keyword := "%" + req.BuyerKeyword + "%"

		query = query.Joins("JOIN customers ON customers.id = sales.customer_id").
			Where("customers.first_name LIKE ? OR customers.last_name LIKE ?", keyword, keyword)
	}

	// Filter by product name
	if req.ProductKeyword != "" {
		keyword := "%" + req.ProductKeyword + "%"

		query = query.Joins("JOIN products ON products.id = sales.product_id").
			Where("products.name LIKE ?", keyword)
	}

	// Filter by Date Range
	if req.StartDate != "" {
		start, _ := utils.ParseFlexibleDate(req.StartDate)
		query = query.Where("sales.date >= ?", start)
	}

	if req.EndDate != "" {
		end, _ := utils.ParseFlexibleDate(req.EndDate)
		query = query.Where("sales.date <= ?", end)
	}

	// Filter by hour of purchase
	if req.StartHour != nil {
		query = query.Where("CAST(strftime('%H', sales.date) AS INT) >= ?", *req.StartHour)
	}

	if req.EndHour != nil {
		query = query.Where("CAST(strftime('%H', sales.date) AS INT) <= ?", *req.EndHour)
	}

	var totalCount int64
	var totalRevenue float64

	if err := query.Count(&totalCount).Error; err != nil {
		return nil, err
	}

	if err := query.Select("sum(amount)").Scan(&totalRevenue).Error; err != nil {
		// If no rows match, Scan might error or return 0. simpler handling:
		totalRevenue = 0
	}

	return &pb.SalesResponse{
		TotalSalesCount: totalCount,
		TotalRevenue:    int64(totalRevenue),
		RegionProcessed: "Worker-50052",
	}, nil
}

func (s *WorkerServer) GetSystemStats(_ context.Context, _ *pb.Empty) (*pb.SystemStats, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return &pb.SystemStats{
		MemoryUsage:      m.Alloc,
		ActiveGoroutines: uint32(runtime.NumGoroutine()),
		CpuUsage:         0.0, // TODO: Add CPU usage tracking
	}, nil
}

func main() {
	database := db.Init("./database.db")

	port := ":50052"

	if len(os.Args) > 1 {
		port = fmt.Sprintf(":%s", os.Args[1])
	}

	listener, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	worker := &WorkerServer{DB: database}

	pb.RegisterSimulationServiceServer(grpcServer, worker)

	log.Printf("RPC Worker running on %s...", port)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
