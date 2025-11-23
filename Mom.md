# 📚 Explicação Completa da Implementação MOM

Vou te explicar **tudo** que foi feito e como rodar!

---

## 🏗️ Arquitetura Implementada

```
┌─────────────┐
│ mom-client  │ (Gera requisições de benchmark)
└──────┬──────┘
       │ Publica mensagens
       ↓
┌─────────────────┐
│   RabbitMQ      │ (Message Broker)
│ sales_requests  │ (Fila de entrada)
└──────┬──────────┘
       │
       ↓
┌─────────────┐
│   mom-lb    │ (Load Balancer)
└──────┬──────┘
       │ Roteia para workers usando "Least Outstanding Requests"
       ↓
┌──────────────────────────────────┐
│  mom-worker-1 | mom-worker-2 | mom-worker-3  │
└──────┬──────────────┬──────────────┬─────────┘
       │              │              │
       └──────────────┴──────────────┘
              Processam queries SQL
              Retornam respostas para client
```

---

## 📝 Explicação de Cada Componente

### **1. mom-client (Cliente de Benchmark)**

**O que faz:**

- Simula carga concorrente (15 conexões simultâneas)
- Envia 400 requisições/segundo durante 30 segundos
- Gera queries aleatórias (região, categoria, range de datas)
- Mede latência e throughput

**Principais correções feitas:**

#### **A) ResponseRouter - Evita Race Condition**

```go
type ResponseRouter struct {
    mu      sync.Mutex
    pending map[string]chan amqp091.Delivery
}
```

**Problema original:** Múltiplas goroutines lendo do mesmo canal `msgs`
**Solução:** Cada requisição registra seu próprio canal usando correlationID

```go
// Antes (ERRADO):
for msg := range msgs {  // Todas goroutines competem por este canal!
    if msg.CorrelationId == corrID {
        // ...
    }
}

// Depois (CORRETO):
respChan := router.Register(corrID)  // Canal exclusivo
select {
case msg := <-respChan:  // Recebe apenas SUA resposta
    // processar
}
```

#### **B) ChannelPool - Thread-Safe**

```go
type ChannelPool struct {
    conn     *amqp091.Connection
    channels chan *amqp091.Channel
}
```

**Problema original:** `ch.Publish()` sendo chamado por múltiplas goroutines
**Solução:** Pool de channels - cada goroutine pega um exclusivo, usa, e devolve

```go
ch := pool.Get()         // Pega channel do pool
defer pool.Put(ch)       // Devolve ao pool
ch.Publish(...)          // Usa com segurança
```

#### **C) CorrelationID Fix**

```go
// Antes (ERRADO):
corrID := "..." + string(123)  // Retorna "...{" (caractere Unicode!)

// Depois (CORRETO):
corrID := "..." + strconv.FormatInt(atomic.AddInt64(&corrIDCounter, 1), 10)
```

---

### **2. mom-worker (Processador de Queries)**

**O que faz:**

- Consome mensagens da sua fila (`mom-worker-1`, `mom-worker-2`, etc)
- Executa queries SQL no SQLite
- Retorna resposta diretamente para o client (via ReplyTo)

**Principais correções:**

#### **A) ChannelPool para Publish**

```go
publishPool, err := NewChannelPool(conn, 20)
```

**Problema original:** Channel compartilhado entre goroutines
**Solução:** Pool de 20 channels para publish thread-safe

#### **B) Prefetch QoS**

```go
err = consumeCh.Qos(10, 0, false)
```

**O que faz:** Limita cada worker a processar no máximo 10 mensagens simultaneamente
**Por quê:** Evita sobrecarga e distribui melhor a carga

#### **C) ACK apenas após sucesso**

```go
err := ch.Publish(...)  // Publica resposta
if err != nil {
    msg.Nack(false, true)  // Erro: requeue para tentar novamente
    return
}
msg.Ack(false)  // Sucesso: confirma processamento
```

#### **D) Suporte a DB_PATH**

```go
dbPath := os.Getenv("DB_PATH")
if dbPath == "" {
    dbPath = "./database.db"
}
database := db.Init(dbPath)
```

**Por quê:** Permite Docker mapear o banco para `/app/database.db`

---

### **3. mom-lb (Load Balancer Assíncrono)**

**O que faz:**

- Consome mensagens da fila `sales_requests` (enviadas pelo client)
- Escolhe o melhor worker (Least Outstanding Requests)
- Roteia a mensagem para a fila do worker
- **Não espera resposta** (assíncrono!)

**Estratégia de Load Balancing:**

#### **Least Outstanding Requests (LOR)**

```go
// Escolhe worker com menos requisições pendentes
var bestWorker *WorkerStats
minPending := int32(999999)

for _, worker := range lb.workers {
    if !worker.isHealthy {
        continue  // Ignora workers não saudáveis
    }
    if worker.pendingCount < minPending {
        minPending = worker.pendingCount
        bestWorker = worker
    }
}
```

**Como funciona:**

1. Incrementa `pendingCount` ao enviar mensagem
2. Decrementa após 500ms (estimativa de processamento)
3. Workers mais rápidos têm menor pending → recebem mais requisições

#### **Round-Robin Tiebreaker**

```go
startIndex := lb.counter % len(lb.workers)
lb.counter++
```

**Quando usa:** Se múltiplos workers têm o mesmo `pendingCount`

#### **Health Check**

```go
func (lb *LBServer) checkWorkerHealth() {
    for _, w := range lb.workers {
        queue, err := lb.ch.QueueInspect(w.queueName)
        if err != nil {
            w.isHealthy = false
            continue
        }
        w.isHealthy = true
    }
}
```

**O que faz:** A cada 5s, verifica se as filas dos workers existem

#### **Comportamento Assíncrono**

```go
// Publica para worker
err := lb.ch.Publish("", bestWorker.queueName, ...)

// ACK imediatamente (NÃO espera resposta!)
msg.Ack(false)

// Decrementa pending após tempo estimado
time.AfterFunc(500*time.Millisecond, func() {
    bestWorker.pendingCount--
})
```

**Por quê assíncrono?**

- ✅ LB não vira gargalo
- ✅ Aproveita o modelo de mensageria
- ✅ Workers respondem direto para o client

---

### **4. Dockerfile**

**O que mudou:**

```dockerfile
# Build Stage
FROM golang:1.24-alpine AS builder
WORKDIR /build
RUN apk add --no-cache git gcc musl-dev sqlite-dev

# Runtime Stage
FROM alpine:latest
RUN apk add --no-cache sqlite-libs  # ← Necessário para CGO

WORKDIR /app  # ← Antes era /root/

# Usuário não-root (segurança)
RUN adduser -D -u 1000 appuser
USER appuser
```

**Melhorias:**

- ✅ WORKDIR consistente (`/app`)
- ✅ Runtime dependencies (sqlite-libs)
- ✅ Usuário não-root
- ✅ Não copia database.db (usa volume)

---

### **5. docker-compose.mom.yml**

**Configurações importantes:**

```yaml
rabbitmq:
  healthcheck: # ← Workers só sobem quando RabbitMQ estiver pronto
    test: rabbitmq-diagnostics -q ping
    interval: 10s

mom-worker-1:
  environment:
    - QUEUE_NAME=mom-worker-1
    - WORKER_ID=mom-worker-1 # ← Identificação nos logs
    - DB_PATH=/app/database.db # ← Caminho do banco
  volumes:
    - ./database.db:/app/database.db:ro # ← Read-only, WAL mode
  user: "1000:1000" # ← Match Dockerfile non-root user
  depends_on:
    rabbitmq:
      condition: service_healthy # ← Espera RabbitMQ
```

---

### **6. SQLite WAL Mode**

**O que é WAL?**

- Write-Ahead Logging
- Permite **múltiplos leitores simultâneos**

**Sem WAL:**

```
Worker-1: SELECT ... [18 segundos bloqueado]
Worker-2: [esperando Worker-1] 🔒
Worker-3: [esperando Worker-1] 🔒
```

**Com WAL:**

```
Worker-1: SELECT ... [35ms] ✅
Worker-2: SELECT ... [42ms] ✅ (simultâneo!)
Worker-3: SELECT ... [38ms] ✅ (simultâneo!)
```

**Como ativar:**

```powershell
sqlite3 database.db "PRAGMA journal_mode=WAL;"
```

---

## 🚀 Comandos para Rodar (Guia Completo)

### **Passo 1: Pré-requisitos**

```powershell
# Instalar dependências
go mod download

# Ter Docker Desktop rodando
# Ter SQLite3 instalado (ou baixar sqlite3.exe)
```

---

### **Passo 2: Gerar o Banco de Dados**

```powershell
# Gerar database.db com dados mockados
go run cmd/seeder/main.go
```

**O que cria:**

- 5 Regiões
- ~100 Produtos
- ~10.000 Vendas
- Índices para performance

---

### **Passo 3: Ativar WAL Mode (CRÍTICO)**

```powershell
# Permitir leitura concorrente
sqlite3 database.db "PRAGMA journal_mode=WAL;"

# Verificar
sqlite3 database.db "PRAGMA journal_mode;"
# Deve retornar: wal
```

---

### **Passo 4: Subir a Infraestrutura MOM**

```powershell
# Build e start dos containers
docker-compose -f docker-compose.mom.yml up --build
```

**O que sobe:**

- 1x RabbitMQ (broker)
- 1x Load Balancer
- 3x Workers

**Esperar ver:**

```
rabbitmq      | Server startup complete
mom-worker-1  | 🟢 MOM Worker [mom-worker-1] listening...
mom-worker-2  | 🟢 MOM Worker [mom-worker-2] listening...
mom-worker-3  | 🟢 MOM Worker [mom-worker-3] listening...
mom-lb        | 🟢 MOM Load Balancer running...
```

---

### **Passo 5: Rodar o Benchmark**

```powershell
# Em um NOVO terminal (deixar docker-compose rodando)
go run cmd/mom-client/main.go
```

**Configurações:**

- `targetRate: 400` → 400 requisições/segundo
- `duration: 30s` → Benchmark de 30 segundos
- `concurrency: 15` → 15 conexões simultâneas

---

### **Passo 6: Ver Resultados**

**No terminal do client:**

```
--- 📊 BENCHMARK RESULTS ---
Total Requests: 12000
Success: 11987 (99.89%)
Failed: 13
Avg Latency: 45.32 ms
Actual RPS: 399.57
```

**No terminal do docker-compose:**

```
mom-lb | --- 📊 Worker Statistics ---
mom-lb | 🟢 mom-worker-1: Pending=3, Total=4021
mom-lb | 🟢 mom-worker-2: Pending=2, Total=3988
mom-lb | 🟢 mom-worker-3: Pending=4, Total=3991
```

**RabbitMQ Management UI:**

```
http://localhost:15672
user: guest
pass: guest
```

---

### **Passo 7: Parar Tudo**

```powershell
# Ctrl+C no terminal do docker-compose
# Ou:
docker-compose -f docker-compose.mom.yml down
```

---

## 📊 Resumo dos Arquivos Alterados

| Arquivo                  | O que mudou                                     |
| ------------------------ | ----------------------------------------------- |
| `cmd/mom-client/main.go` | ResponseRouter, ChannelPool, CorrelationID fix  |
| `cmd/mom-worker/main.go` | ChannelPool, Prefetch QoS, DB_PATH, ACK correto |
| `cmd/mom-lb/main.go`     | LOR assíncrono, Health check, Round-robin       |
| `Dockerfile`             | WORKDIR /app, non-root user, runtime deps       |
| `docker-compose.mom.yml` | Health checks, env vars corretas, depends_on    |
| `database.db`            | WAL mode ativado                                |

---

## 🎯 Comandos Rápidos (Cheat Sheet)

```powershell
# Setup inicial (uma vez)
go run cmd/seeder/main.go
sqlite3 database.db "PRAGMA journal_mode=WAL;"

# Rodar benchmark
docker-compose -f docker-compose.mom.yml up --build
# (novo terminal)
go run cmd/mom-client/main.go

# Parar
docker-compose -f docker-compose.mom.yml down

# Limpar tudo
docker-compose -f docker-compose.mom.yml down -v
docker system prune -af
```

---

Tudo explicado! Alguma dúvida? 🚀
