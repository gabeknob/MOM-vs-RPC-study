# BUILD
FROM golang:1.24-alpine AS builder
LABEL authors="gabrielnobrega"

WORKDIR /app

# Adding gcc is necessary to access SQLite
RUN apk add --no-cache git gcc musl-dev

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd/
COPY pkg ./pkg/

ARG APP_NAME
# CGO_ENABLED=1 for enabling C access to SQLite
RUN CGO_ENABLED=1 go build  -o /bin/app ./cmd/${APP_NAME}

# RUN
FROM alpine:latest

WORKDIR /root/

COPY --from=builder /bin/app .

COPY database.db .

CMD ["./app"]