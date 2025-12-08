FROM golang:1.21-alpine AS builder

WORKDIR /app
RUN apk add --no-cache build-base

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the server binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/server ./cmd/server

# ---- Runtime image ----
FROM alpine:3.19
RUN apk add --no-cache ca-certificates netcat-openbsd
WORKDIR /app

COPY --from=builder /app/server /app/server
COPY entrypoint.sh /app/entrypoint.sh

RUN chmod +x /app/entrypoint.sh

# Expose gRPC and RAFT ports
EXPOSE 50051 12000

# Data directory for persistence
VOLUME ["/data"]

ENTRYPOINT ["/app/entrypoint.sh"]

