package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"simulation/pkg/db"
	"simulation/pkg/mom"
	"simulation/pkg/utils"

	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"
)

func processQuery(DB *gorm.DB, req mom.SalesRequest) (mom.SalesResponse, error) {
	query := DB.Model(&db.Sale{})

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
		return mom.SalesResponse{}, err
	}

	if err := query.Select("sum(amount)").Scan(&totalRevenue).Error; err != nil {
		// If no rows match, Scan might error or return 0. simpler handling:
		totalRevenue = 0
	}

	return mom.SalesResponse{
		TotalSalesCount: totalCount,
		TotalRevenue:    int64(totalRevenue),
		RegionProcessed: "MOM-Worker-Placeholder",
	}, nil
}

func main() {
	database := db.Init("./database.db")
	rabbitUrl := os.Getenv("RABBITMQ_URL")
	if rabbitUrl == "" {
		rabbitUrl = "amqp://guest:guest@localhost:5672/"
	}

	conn, err := amqp.Dial(rabbitUrl)
	mom.FailOnError(err, "Failed to connect to RabbitMQ")
	defer func(conn *amqp.Connection) {
		err := conn.Close()
		if err != nil {
			log.Fatalf("Failed to close RabbitMQ connection")
		}
	}(conn)

	ch, err := conn.Channel()
	mom.FailOnError(err, "Failed to open a channel")
	defer func(ch *amqp.Channel) {
		err := ch.Close()
		if err != nil {
			log.Fatalf("Failed to close channel")
		}
	}(ch)

	q, err := ch.QueueDeclare(
		"worker_queue", // queue name; idempotent
		false,          // store everything in a volume so it's durable
		false,          // deletes queue if no one is subscribed
		false,          // exclusive
		false,          // no wait for server to confirm request
		nil,            // args
	)
	mom.FailOnError(err, "Failed to declare a queue")

	err = ch.Qos(1, 0, false)
	mom.FailOnError(err, "Failed to set QoS")

	msgs, err := ch.Consume(
		q.Name,
		"",    // auto-declared
		false, // we do manual ack
		false, // exclusive
		false, // no receiving messages from this publisher
		false, // no wait for server to confirm request
		nil,   // args
	)
	mom.FailOnError(err, "Failed to register a consumer")

	log.Printf("🐰 MOM Worker waiting for messages on %s", q.Name)

	forever := make(chan struct{})

	go func() {
		for d := range msgs {
			var req mom.SalesRequest
			err := json.Unmarshal(d.Body, &req)

			if err != nil {
				log.Printf("Error decoding JSON: %s", err)
				err := d.Ack(false) // Ack to notify errors
				if err != nil {
					log.Printf("Error acknowledging message: %s", err)
					return
				}
				continue
			}

			response, err := processQuery(database, req)
			if err != nil {
				log.Printf("Error processing query: %s", err)
			}
			responseBody, _ := json.Marshal(response)

			err = ch.PublishWithContext(
				context.Background(),
				"", // exchange (default)
				d.ReplyTo,
				false, // mandatory - answer only to publisher
				false, // immediate - deprecated
				amqp.Publishing{ // the message
					ContentType:   "application/json",
					CorrelationId: d.CorrelationId, // Echo the ID back so sender knows which request this is
					Body:          responseBody,
				},
			)

			err = d.Ack(false) // Finish request ack
			if err != nil {
				log.Printf("Error acknowledging message: %s", err)
				return
			}
		}
	}()

	<-forever
}
