package main

import (
	"context"
	"log"
	"math/rand"
	"os"
	"simulation/pkg/mom"
	"sync"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

type MomLoadBalancer struct {
	conn *amqp.Connection

	pubChannels []*amqp.Channel
	numChannels int

	pendingReqs map[string]chan []byte
	mu          sync.Mutex
}

func (lb *MomLoadBalancer) StartReplyListener() {
	// FIX: Iterate over ALL channels, not just [0]
	for _, ch := range lb.pubChannels {
		// 1. Start Consuming on THIS specific channel
		msgs, err := ch.Consume(
			"amq.rabbitmq.reply-to", // queue
			"",                      // consumer
			true,                    // auto-ack (Required for direct reply-to)
			false,                   // exclusive
			false,                   // no-local
			false,                   // no-wait
			nil,                     // args
		)
		mom.FailOnError(err, "Failed to consume reply queue")

		// 2. Start a background routine for THIS channel
		go func(deliveries <-chan amqp.Delivery) {
			for d := range deliveries {
				// Look up who is waiting for this ID in the SHARED map
				lb.mu.Lock()
				respChan, ok := lb.pendingReqs[d.CorrelationId]
				if ok {
					// Clean up immediately
					delete(lb.pendingReqs, d.CorrelationId)
				}
				lb.mu.Unlock()

				if ok {
					respChan <- d.Body
				}
			}
		}(msgs)
	}
}

func (lb *MomLoadBalancer) ProcessRequest(d amqp.Delivery) {
	pubChan := lb.pubChannels[rand.Intn(lb.numChannels)]

	// Generate Internal ID for LB->Worker leg
	internalID := uuid.New().String()

	waitChan := make(chan []byte, 1)

	lb.mu.Lock()
	lb.pendingReqs[internalID] = waitChan
	lb.mu.Unlock()

	err := pubChan.PublishWithContext(
		context.Background(),
		"",
		"worker_queue",
		false, false,
		amqp.Publishing{
			ContentType:   d.ContentType,
			CorrelationId: internalID,
			ReplyTo:       "amq.rabbitmq.reply-to", // return to LB
			Body:          d.Body,                  // forward data
		})

	if err != nil {
		log.Printf("Failed to forward: %v", err)

		err := d.Nack(false, false)
		if err != nil {
			log.Printf("Failed to nack: %v", err)
		}

		return
	}

	responseBody := <-waitChan
	err = pubChan.PublishWithContext(
		context.Background(),
		"",
		d.ReplyTo, // CLIENT'S Address
		false, false,
		amqp.Publishing{
			ContentType:   "application/json",
			CorrelationId: d.CorrelationId,
			Body:          responseBody,
		})

	if err != nil {
		log.Printf("Failed to reply to client: %v", err)
	} else {
		err := d.Ack(false) // success
		if err != nil {
			log.Printf("Failed to ack: %v", err)
		}
	}
}

func main() {
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

	mlb := &MomLoadBalancer{
		conn:        conn,
		numChannels: 16,
		pendingReqs: make(map[string]chan []byte),
		pubChannels: make([]*amqp.Channel, 0),
	}

	log.Println("Creating channel pool...")
	for i := 0; i < mlb.numChannels; i++ {
		ch, err := mlb.conn.Channel()
		mom.FailOnError(err, "Failed to open a channel")
		mlb.pubChannels = append(mlb.pubChannels, ch)
	}

	ch, err := mlb.conn.Channel()
	mom.FailOnError(err, "Failed to open a channel")
	defer func(ch *amqp.Channel) {
		err := ch.Close()
		if err != nil {
			log.Printf("Error closing RabbitMQ channel: %s", err)
		}
	}(ch)

	lbQueue, err := ch.QueueDeclare(
		"lb_queue",
		false, false, false, false, nil,
	)
	mom.FailOnError(err, "Failed to declare a queue")

	mlb.StartReplyListener()

	clientMsgs, err := ch.Consume(
		lbQueue.Name,
		"",
		false, false, false, false, nil,
	)
	mom.FailOnError(err, "Failed to register a consumer")

	log.Printf("Load balancer waiting for messages")

	// Infinite execution code
	forever := make(chan struct{})

	go func(deliveries <-chan amqp.Delivery) {
		for msg := range deliveries {
			go mlb.ProcessRequest(msg)
		}
	}(clientMsgs)

	<-forever
}
