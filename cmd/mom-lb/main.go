package main

import (
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

type WorkerStats struct {
	queueName      string
	pendingCount   int32
	totalProcessed uint64
	isHealthy      bool
	lastSeen       time.Time
}

type LBServer struct {
	workers       []*WorkerStats
	workersByName map[string]*WorkerStats
	mu            sync.RWMutex
	counter       int

	conn *amqp091.Connection
	ch   *amqp091.Channel
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}

func NewLBServer(workerQueues []string, amqpURL string) *LBServer {
	conn, err := amqp091.Dial(amqpURL)
	failOnError(err, "Failed to connect to RabbitMQ")

	ch, err := conn.Channel()
	failOnError(err, "Failed to open channel")

	// Declarar fila de entrada dos clientes
	_, err = ch.QueueDeclare("sales_requests", true, false, false, false, nil)
	failOnError(err, "Failed to declare sales_requests queue")

	lb := &LBServer{
		workers:       make([]*WorkerStats, 0, len(workerQueues)),
		workersByName: make(map[string]*WorkerStats),
		conn:          conn,
		ch:            ch,
	}

	// Inicializar workers
	for _, wq := range workerQueues {
		// Declarar fila do worker
		_, err := ch.QueueDeclare(wq, true, false, false, false, nil)
		failOnError(err, "Failed to declare worker queue: "+wq)

		stats := &WorkerStats{
			queueName:      wq,
			pendingCount:   0,
			totalProcessed: 0,
			isHealthy:      true,
			lastSeen:       time.Now(),
		}

		lb.workers = append(lb.workers, stats)
		lb.workersByName[wq] = stats
	}

	log.Printf("Load Balancer initialized with %d workers: %v", len(workerQueues), workerQueues)
	return lb
}

func (lb *LBServer) Start() {
	// Configurar prefetch para evitar sobrecarga do LB
	err := lb.ch.Qos(100, 0, false)
	failOnError(err, "Failed to set QoS")

	msgs, err := lb.ch.Consume("sales_requests", "", false, false, false, false, nil)
	failOnError(err, "Failed to consume sales_requests")

	// Goroutine para monitoramento periódico
	go lb.monitor()

	log.Println("🟢 MOM Load Balancer running, waiting for messages...")

	// Processar mensagens
	for msg := range msgs {
		lb.handleRequest(msg)
	}
}

func (lb *LBServer) handleRequest(msg amqp091.Delivery) {
	lb.mu.Lock()

	// Escolher worker com menos requisições pendentes (Least Outstanding Requests)
	var bestWorker *WorkerStats
	minPending := int32(999999)

	// Round-robin para tiebreak
	startIndex := lb.counter % len(lb.workers)
	lb.counter++

	healthyCount := 0
	for i := 0; i < len(lb.workers); i++ {
		idx := (startIndex + i) % len(lb.workers)
		worker := lb.workers[idx]

		if !worker.isHealthy {
			continue
		}

		healthyCount++
		if worker.pendingCount < minPending {
			minPending = worker.pendingCount
			bestWorker = worker
		}
	}

	if healthyCount == 0 {
		lb.mu.Unlock()
		log.Println("❌ No healthy workers available, requeuing message")
		msg.Nack(false, true)
		return
	}

	// Incrementar pending ANTES de enviar
	bestWorker.pendingCount++
	bestWorker.totalProcessed++
	queueName := bestWorker.queueName

	lb.mu.Unlock()

	// Publicar para a fila do worker escolhido (NÃO espera resposta!)
	err := lb.ch.Publish("", queueName, false, false, amqp091.Publishing{
		ContentType:   "application/json",
		CorrelationId: msg.CorrelationId,
		ReplyTo:       msg.ReplyTo, // Worker envia direto para o client
		Body:          msg.Body,
		DeliveryMode:  amqp091.Persistent,
	})

	if err != nil {
		log.Printf("❌ Failed to forward to %s: %v", queueName, err)
		lb.mu.Lock()
		bestWorker.pendingCount-- // Reverter incremento
		lb.mu.Unlock()
		msg.Nack(false, true)
		return
	}

	// ACK imediatamente (assíncrono!)
	msg.Ack(false)

	log.Printf("✅ Routed to %s (Pending: %d, Total: %d, corrID: %s)",
		queueName, minPending, bestWorker.totalProcessed, msg.CorrelationId)

	// Decrementar pending após um tempo estimado (ex: 500ms)
	// Isso mantém o contador útil mesmo sem feedback direto
	time.AfterFunc(500*time.Millisecond, func() {
		lb.mu.Lock()
		if bestWorker.pendingCount > 0 {
			bestWorker.pendingCount--
		}
		lb.mu.Unlock()
	})
}

func (lb *LBServer) monitor() {
	reportTicker := time.NewTicker(10 * time.Second)
	healthTicker := time.NewTicker(5 * time.Second)

	for {
		select {
		case <-reportTicker.C:
			lb.printStats()

		case <-healthTicker.C:
			lb.checkWorkerHealth()
		}
	}
}

func (lb *LBServer) printStats() {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	log.Println("--- 📊 Worker Statistics ---")
	for _, w := range lb.workers {
		status := "🟢"
		if !w.isHealthy {
			status = "🔴"
		}
		log.Printf("%s %s: Pending=%d, Total=%d",
			status, w.queueName, w.pendingCount, w.totalProcessed)
	}
}

func (lb *LBServer) checkWorkerHealth() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	// Verificar filas via AMQP Inspect (básico)
	for _, w := range lb.workers {
		queue, err := lb.ch.QueueInspect(w.queueName)
		if err != nil {
			log.Printf("⚠️ Health check failed for %s: %v", w.queueName, err)
			w.isHealthy = false
			continue
		}

		// Se a fila existe e não está fechada, considerar saudável
		w.isHealthy = true
		w.lastSeen = time.Now()

		// Ajustar pending count baseado no tamanho da fila
		// (aproximação, não é perfeito mas evita drift)
		if queue.Messages > 0 {
			w.pendingCount = int32(queue.Messages)
		}
	}
}

func (lb *LBServer) Close() {
	lb.ch.Close()
	lb.conn.Close()
}

func main() {
	workerList := strings.Split(os.Getenv("WORKER_QUEUES"), ",")
	if len(workerList) == 0 || workerList[0] == "" {
		log.Fatal("❌ No worker queues provided in WORKER_QUEUES env")
	}

	amqpURL := os.Getenv("RABBITMQ_URL")
	if amqpURL == "" {
		amqpURL = "amqp://guest:guest@localhost:5672/"
	}

	lb := NewLBServer(workerList, amqpURL)
	defer lb.Close()

	lb.Start()
}