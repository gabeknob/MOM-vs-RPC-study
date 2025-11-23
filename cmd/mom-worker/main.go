package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"

	pb "simulation/pkg/protocol"
	"simulation/pkg/db"
)

func int32ptr(n int32) *int32 { return &n }

// ChannelPool para publicar responses de forma thread-safe
type ChannelPool struct {
	conn     *amqp091.Connection
	channels chan *amqp091.Channel
	mu       sync.Mutex
}

func NewChannelPool(conn *amqp091.Connection, size int) (*ChannelPool, error) {
	pool := &ChannelPool{
		conn:     conn,
		channels: make(chan *amqp091.Channel, size),
	}

	for i := 0; i < size; i++ {
		ch, err := conn.Channel()
		if err != nil {
			return nil, fmt.Errorf("failed to create channel: %v", err)
		}
		pool.channels <- ch
	}

	return pool, nil
}

func (p *ChannelPool) Get() *amqp091.Channel {
	return <-p.channels
}

func (p *ChannelPool) Put(ch *amqp091.Channel) {
	p.channels <- ch
}

func (p *ChannelPool) Close() {
	close(p.channels)
	for ch := range p.channels {
		ch.Close()
	}
}

func main() {
	workerID := os.Getenv("WORKER_ID")
	if workerID == "" {
		workerID = "worker-unknown"
	}

	// Conecta no DB
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./database.db"
	}
	database := db.Init(dbPath)

	// Conecta no RabbitMQ
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@localhost:5672/"
	}

	conn, err := amqp091.Dial(rabbitURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	// Channel para consumir
	consumeCh, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open consume channel: %v", err)
	}
	defer consumeCh.Close()

	// Configurar Prefetch para limitar concorrência
	err = consumeCh.Qos(10, 0, false)
	if err != nil {
		log.Fatalf("Failed to set QoS: %v", err)
	}

	// Fila de requests
	queueName := os.Getenv("QUEUE_NAME")
	if queueName == "" {
		queueName = "sales_requests"
	}

	_, err = consumeCh.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}

	msgs, err := consumeCh.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to consume queue: %v", err)
	}

	// Criar channel pool para publish (thread-safe)
	publishPool, err := NewChannelPool(conn, 20)
	if err != nil {
		log.Fatalf("Failed to create publish pool: %v", err)
	}
	defer publishPool.Close()

	log.Printf("🟢 MOM Worker [%s] listening on queue: %s", workerID, queueName)

	// Processar mensagens
	for msg := range msgs {
		go handleMessage(msg, database, publishPool, workerID)
	}
}

func handleMessage(msg amqp091.Delivery, database *gorm.DB, pool *ChannelPool, workerID string) {
	start := time.Now()

	var req pb.SalesRequest
	if err := json.Unmarshal(msg.Body, &req); err != nil {
		log.Printf("❌ Invalid request: %v", err)
		msg.Nack(false, false) // Não requeue mensagens inválidas
		return
	}

	// Processar query
	resp := processSalesRequest(database, &req, workerID)

	// Publicar resposta usando channel do pool
	ch := pool.Get()
	defer pool.Put(ch)

	body, _ := json.Marshal(resp)
	err := ch.Publish("", msg.ReplyTo, false, false, amqp091.Publishing{
		ContentType:   "application/json",
		CorrelationId: msg.CorrelationId,
		Body:          body,
	})

	if err != nil {
		log.Printf("❌ Failed to publish response: %v", err)
		msg.Nack(false, true) // Requeue para tentar novamente
		return
	}

	// ACK apenas após publicar resposta com sucesso
	msg.Ack(false)

	elapsed := time.Since(start).Milliseconds()
	log.Printf("✅ [%s] Processed request (corrID: %s) in %d ms - Count: %d",
		workerID, msg.CorrelationId, elapsed, resp.TotalSalesCount)
}

func processSalesRequest(database *gorm.DB, req *pb.SalesRequest, workerID string) *pb.SalesResponse {
	query := database.Model(&db.Sale{})

	// Filter by Region
	if req.Region != "" {
		query = query.Joins("JOIN stores ON stores.id = sales.store_id").
			Joins("JOIN regions ON regions.id = stores.region_id").
			Where("regions.name = ?", req.Region)
	}

	// Filter by Category
	if req.Category != "" {
		query = query.Joins("JOIN products ON products.id = sales.product_id").
			Where("products.category = ?", req.Category)
	}

	// Filter by buyer name
	if req.BuyerKeyword != "" {
		kw := "%" + req.BuyerKeyword + "%"
		query = query.Joins("JOIN customers ON customers.id = sales.customer_id").
			Where("customers.first_name LIKE ? OR customers.last_name LIKE ?", kw, kw)
	}

	// Filter by product name
	if req.ProductKeyword != "" {
		kw := "%" + req.ProductKeyword + "%"
		query = query.Joins("JOIN products ON products.id = sales.product_id").
			Where("products.name LIKE ?", kw)
	}

	// Filter by date range
	if req.StartDate != "" {
		start, err := parseFlexibleDate(req.StartDate)
		if err == nil {
			query = query.Where("sales.date >= ?", start)
		}
	}
	if req.EndDate != "" {
		end, err := parseFlexibleDate(req.EndDate)
		if err == nil {
			query = query.Where("sales.date <= ?", end)
		}
	}

	// Filter by hour
	if req.StartHour != nil {
		query = query.Where("CAST(strftime('%H', sales.date) AS INT) >= ?", *req.StartHour)
	}
	if req.EndHour != nil {
		query = query.Where("CAST(strftime('%H', sales.date) AS INT) <= ?", *req.EndHour)
	}

	var totalCount int64
	var totalRevenue float64

	if err := query.Count(&totalCount).Error; err != nil {
		log.Printf("Count error: %v", err)
	}

	if err := query.Select("COALESCE(sum(amount), 0)").Scan(&totalRevenue).Error; err != nil {
		log.Printf("Sum error: %v", err)
		totalRevenue = 0
	}

	avg := float64(0)
	if totalCount > 0 {
		avg = totalRevenue / float64(totalCount)
	}

	return &pb.SalesResponse{
		TotalSalesCount: totalCount,
		TotalRevenue:    int64(totalRevenue),
		AverageAmount:   avg,
		RegionProcessed: workerID,
	}
}

func parseFlexibleDate(dateStr string) (time.Time, error) {
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, dateStr); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}