package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rabbitmq/amqp091-go"
	pb "simulation/pkg/protocol"
)

func int32ptr(n int32) *int32 {
	return &n
}

// ResponseRouter gerencia responses por correlationID
type ResponseRouter struct {
	mu      sync.Mutex
	pending map[string]chan amqp091.Delivery
}

func NewResponseRouter() *ResponseRouter {
	return &ResponseRouter{
		pending: make(map[string]chan amqp091.Delivery),
	}
}

func (r *ResponseRouter) Register(corrID string) chan amqp091.Delivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan amqp091.Delivery, 1)
	r.pending[corrID] = ch
	return ch
}

func (r *ResponseRouter) Unregister(corrID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pending, corrID)
}

func (r *ResponseRouter) Route(msg amqp091.Delivery) {
	r.mu.Lock()
	ch, ok := r.pending[msg.CorrelationId]
	r.mu.Unlock()

	if ok {
		select {
		case ch <- msg:
		default:
			// Canal cheio ou fechado, ignorar
		}
	}
}

// ChannelPool gerencia um pool de channels thread-safe
type ChannelPool struct {
	conn     *amqp091.Connection
	channels chan *amqp091.Channel
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
	// Não fecha o pool - deixa Go GC limpar
	// Evita panic de "send on closed channel"
}

func main() {
	targetRate := 400
	duration := 30 * time.Second
	concurrency := 15

	var successCount int64
	var failCount int64
	var totalLatency int64
	var corrIDCounter int64

	amqpURL := os.Getenv("RABBITMQ_URL")
	if amqpURL == "" {
		amqpURL = "amqp://guest:guest@localhost:5672/"
	}

	conn, err := amqp091.Dial(amqpURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	// Criar channel pool (concurrency + buffer)
	pool, err := NewChannelPool(conn, concurrency+5)
	if err != nil {
		log.Fatalf("Failed to create channel pool: %v", err)
	}
	// Não fechar pool - deixa Go GC limpar para evitar panic

	// Criar fila de respostas exclusiva e temporária
	ch := pool.Get()
	replyQueue, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		log.Fatalf("Failed to declare reply queue: %v", err)
	}

	msgs, err := ch.Consume(replyQueue.Name, "", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to consume reply queue: %v", err)
	}
	pool.Put(ch)

	// Response router
	router := NewResponseRouter()

	// Goroutine que roteia responses para os waiters corretos
	go func() {
		for msg := range msgs {
			router.Route(msg)
		}
	}()

	log.Printf("Starting benchmark: %d RPS, %d concurrent connections, %v duration",
		targetRate, concurrency, duration)

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		defer wg.Done()

		interval := time.Second / time.Duration(targetRate)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		timer := time.NewTimer(duration)
		defer timer.Stop()

		for {
			select {
			case <-timer.C:
				return
			case <-ticker.C:
				go func() {
					start := time.Now()

					// Gerar request aleatório
					rangeDateStart := time.Now().Add(time.Hour * -time.Duration(rand.Intn(365*5*24)))
					rangeDateEnd := time.Now().Add(time.Hour * -time.Duration(rand.Intn(int(time.Now().Sub(rangeDateStart).Hours()+1))))
					hourStart := rand.Intn(23)
					hourEnd := hourStart + rand.Intn(24-hourStart)

					req := pb.SalesRequest{
						Region:    "Europe",
						Category:  "Computers",
						StartDate: rangeDateStart.Format("2006-01-02"),
						EndDate:   rangeDateEnd.Format("2006-01-02"),
						StartHour: int32ptr(int32(hourStart)),
						EndHour:   int32ptr(int32(hourEnd)),
					}

					body, _ := json.Marshal(req)

					// Gerar correlationID único (FIX: usar strconv ao invés de string())
					corrID := time.Now().Format("20060102150405.000000") + "-" +
						strconv.FormatInt(atomic.AddInt64(&corrIDCounter, 1), 10)

					// Registrar antes de publicar
					respChan := router.Register(corrID)
					defer router.Unregister(corrID)

					// Pegar channel do pool
					ch := pool.Get()
					defer pool.Put(ch)

					// Publicar na fila do Load Balancer
					err := ch.Publish("", "sales_requests", false, false, amqp091.Publishing{
						ContentType:   "application/json",
						CorrelationId: corrID,
						ReplyTo:       replyQueue.Name,
						Body:          body,
					})
					if err != nil {
						atomic.AddInt64(&failCount, 1)
						log.Printf("Failed to publish request: %v", err)
						return
					}

					// Esperar resposta com timeout
					select {
					case msg := <-respChan:
						var resp pb.SalesResponse
						if err := json.Unmarshal(msg.Body, &resp); err != nil {
							atomic.AddInt64(&failCount, 1)
							log.Printf("Failed to unmarshal response: %v", err)
							return
						}

						atomic.AddInt64(&successCount, 1)
						elapsed := time.Since(start).Milliseconds()
						atomic.AddInt64(&totalLatency, elapsed)

						log.Printf("✅ Response: %s (Count: %d, Revenue: %d) in %d ms",
							resp.RegionProcessed,
							resp.TotalSalesCount,
							resp.TotalRevenue,
							elapsed)

					case <-time.After(10 * time.Second):
						atomic.AddInt64(&failCount, 1)
						log.Printf("❌ Request timed out (corrID: %s)", corrID)
					}
				}()
			}
		}
	}()

	wg.Wait()

	// Aguardar respostas pendentes antes de fechar
	time.Sleep(2 * time.Second)

	avgLatency := 0.0
	if successCount > 0 {
		avgLatency = float64(totalLatency) / float64(successCount)
	}

	log.Println("\n--- 📊 BENCHMARK RESULTS ---")
	log.Printf("Total Requests: %d", successCount+failCount)
	log.Printf("Success: %d (%.2f%%)", successCount, float64(successCount)/float64(successCount+failCount)*100)
	log.Printf("Failed: %d", failCount)
	log.Printf("Avg Latency: %.2f ms", avgLatency)
	log.Printf("Actual RPS: %.2f", float64(successCount)/duration.Seconds())
}