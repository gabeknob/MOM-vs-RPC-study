package main

import (
	"context"
	"encoding/json"
	"log"
	"math/rand/v2"
	"os"
	"simulation/pkg/mom"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

type MomClient struct {
	conn     *amqp.Connection
	channels []*amqp.Channel

	pendingReqs map[string]chan mom.SalesResponse
	mu          sync.Mutex
}

func (c *MomClient) StartListeners() {
	for _, ch := range c.channels {
		msgs, err := ch.Consume(
			"amq.rabbitmq.reply-to", // queue
			"",                      // consumer,
			true,                    // auto ack
			false,                   // exclusive - no need
			false,                   // no local - no need
			false,                   // no wait
			nil,                     // args
		)
		if err != nil {
			log.Fatalf("Failed to consume on channel: %v", err)
		}

		go func(deliveries <-chan amqp.Delivery) {
			for d := range deliveries {
				corrId := d.CorrelationId

				c.mu.Lock()
				respChan, ok := c.pendingReqs[corrId]
				c.mu.Unlock()

				if ok {
					var resp mom.SalesResponse
					err := json.Unmarshal(d.Body, &resp)
					if err != nil {
						log.Printf("Error decoding JSON: %s", err)
					}
					respChan <- resp
				}
			}
		}(msgs)
	}
}

func (c *MomClient) PublishRequest(ctx context.Context, ch *amqp.Channel, req mom.SalesRequest) (mom.SalesResponse, error) {
	corrId := uuid.New().String()

	respChan := make(chan mom.SalesResponse, 1)

	c.mu.Lock()
	c.pendingReqs[corrId] = respChan
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pendingReqs, corrId)
		c.mu.Unlock()
	}()

	body, err := json.Marshal(req)
	if err != nil {
		log.Printf("Error encoding JSON: %s", err)
	}

	err = ch.PublishWithContext(
		ctx,
		"",
		"lb_queue",
		false,
		false,
		amqp.Publishing{
			ContentType:   "application/json",
			Body:          body,
			CorrelationId: corrId,
			ReplyTo:       "amq.rabbitmq.reply-to",
		},
	)
	if err != nil {
		return mom.SalesResponse{}, err
	}

	select {
	case resp := <-respChan:
		return resp, nil
	case <-ctx.Done():
		return mom.SalesResponse{}, ctx.Err()
	}
}

func int32ptr(n int32) *int32 {
	return &n
}

func main() {
	targetRate := 1000
	duration := 30 * time.Second
	concurrency := 15

	rabbitUrl := os.Getenv("RABBITMQ_URL")
	if rabbitUrl == "" {
		rabbitUrl = "amqp://guest:guest@localhost:5672/"
	}

	conn, err := amqp.Dial(rabbitUrl)
	mom.FailOnError(err, "Failed to connect to RabbitMQ")
	defer func(conn *amqp.Connection) {
		err := conn.Close()
		if err != nil {
			log.Printf("Error closing RabbitMQ connection: %s", err)
		}
	}(conn)

	mc := &MomClient{
		conn:        conn,
		pendingReqs: make(map[string]chan mom.SalesResponse),
		channels:    make([]*amqp.Channel, 0, concurrency),
	}

	log.Println("Connecting channels...")
	for i := 0; i < concurrency; i++ {
		ch, err := mc.conn.Channel()
		mom.FailOnError(err, "Failed to open a channel")

		mc.channels = append(mc.channels, ch)
	}

	defer func(channels []*amqp.Channel) {
		for _, channel := range mc.channels {
			err := channel.Close()
			if err != nil {
				log.Println("Failed to close channel")
			}
		}
	}(mc.channels)

	var successCount int64
	var failCount int64
	var totalLatency int64

	mc.StartListeners()

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
				go func(ch *amqp.Channel) {
					start := time.Now()
					//ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					//defer cancel()

					//region := db.RegionNames[rand.IntN(len(db.RegionNames))]
					//category := db.ProductCategories[rand.IntN(len(db.ProductCategories))]
					rangeDateStart := time.Now().Add(time.Hour * -time.Duration(rand.IntN(365*5)*24))
					rangeDateEnd := time.Now().Add(time.Hour * -time.Duration(rand.IntN(int(time.Now().Sub(rangeDateStart).Hours()+1))))
					hourRangeStart := rand.IntN(23)
					hourRangeEnd := rand.IntN(23-hourRangeStart+1) + hourRangeStart + 1

					req := mom.SalesRequest{
						Region:    "Europe",
						Category:  "Computers",
						StartDate: rangeDateStart.Format("2006-01-02"),
						EndDate:   rangeDateEnd.Format("2006-01-02"),
						StartHour: int32ptr(int32(hourRangeStart)),
						EndHour:   int32ptr(int32(hourRangeEnd)),
					}
					res, err := mc.PublishRequest(context.Background(), ch, req)
					//res, err := mc.PublishRequest(ctx, ch, req)

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
				}(mc.channels[clientIdx%concurrency])

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
