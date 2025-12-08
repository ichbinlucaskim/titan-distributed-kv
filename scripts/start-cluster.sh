#!/bin/bash

# Script to start a 3-node RAFT cluster

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}Starting Titan-KV RAFT Cluster (3 nodes)...${NC}"

# Create data directories and logs
mkdir -p data/node1 data/node2 data/node3 logs

# Kill any existing processes on these ports
echo -e "${YELLOW}Cleaning up existing processes...${NC}"
pkill -f "cmd/server/main.go" || true
pkill -f "raft-addr.*:12000" || true
pkill -f "raft-addr.*:12001" || true
pkill -f "raft-addr.*:12002" || true
sleep 2

# Start Node 1 (Bootstrap)
echo -e "${GREEN}Starting Node 1 (Bootstrap Leader)...${NC}"
go run cmd/server/main.go \
  -node-id node1 \
  -grpc-addr :50051 \
  -raft-addr :12000 \
  -data-dir ./data/node1 \
  -bootstrap \
  > logs/node1.log 2>&1 &
NODE1_PID=$!

sleep 3

# Start Node 2
echo -e "${GREEN}Starting Node 2...${NC}"
go run cmd/server/main.go \
  -node-id node2 \
  -grpc-addr :50052 \
  -raft-addr :12001 \
  -data-dir ./data/node2 \
  > logs/node2.log 2>&1 &
NODE2_PID=$!

sleep 2

# Start Node 3
echo -e "${GREEN}Starting Node 3...${NC}"
go run cmd/server/main.go \
  -node-id node3 \
  -grpc-addr :50053 \
  -raft-addr :12002 \
  -data-dir ./data/node3 \
  > logs/node3.log 2>&1 &
NODE3_PID=$!

sleep 3

echo -e "${BLUE}Cluster started!${NC}"
echo ""
echo "Node 1: gRPC :50051, RAFT :12000 (PID: $NODE1_PID)"
echo "Node 2: gRPC :50052, RAFT :12001 (PID: $NODE2_PID)"
echo "Node 3: gRPC :50053, RAFT :12002 (PID: $NODE3_PID)"
echo ""
echo "Logs are in logs/node*.log"
echo ""
echo "Test the cluster:"
echo "  go run cmd/cli/main.go -address localhost:50051"
echo ""
echo "To stop the cluster, run: ./scripts/stop-cluster.sh"
echo "Or press Ctrl+C"

# Wait for interrupt
trap "echo ''; echo 'Stopping cluster...'; kill $NODE1_PID $NODE2_PID $NODE3_PID 2>/dev/null; sleep 1; exit" INT TERM

wait

