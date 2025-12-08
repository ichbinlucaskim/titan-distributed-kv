.PHONY: proto build server cli clean

# Generate protobuf code
proto:
	@echo "Generating protobuf code..."
	@protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/kvstore.proto

# Build all binaries
build: proto
	@echo "Building binaries..."
	@go build -o bin/server ./cmd/server
	@go build -o bin/cli ./cmd/cli

# Build server only
server: proto
	@go build -o bin/server ./cmd/server

# Build CLI only
cli: proto
	@go build -o bin/cli ./cmd/cli

# Clean build artifacts
clean:
	@rm -rf bin/
	@rm -rf proto/kvstore/*.pb.go

# Install dependencies
deps:
	@go mod download
	@go mod tidy

# Run tests
test:
	@go test ./...

