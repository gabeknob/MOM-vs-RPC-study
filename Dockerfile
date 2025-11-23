# ================================
# BUILD STAGE
# ================================
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Install build dependencies (gcc needed for CGO/SQLite)
RUN apk add --no-cache git gcc musl-dev sqlite-dev

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd ./cmd/
COPY pkg ./pkg/

# Build the application
# ARG is passed from docker-compose
ARG APP_NAME
RUN if [ -z "$APP_NAME" ]; then echo "ERROR: APP_NAME not set" && exit 1; fi

# CGO_ENABLED=1 is required for SQLite
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo \
    -ldflags '-extldflags "-static"' \
    -o /bin/app ./cmd/${APP_NAME}

# ================================
# RUNTIME STAGE
# ================================
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates sqlite-libs

# Create app directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /bin/app .

# Security: Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser && \
    chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# The database.db will be mounted via volume
# No need to COPY it here

CMD ["./app"]