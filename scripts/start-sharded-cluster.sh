#!/bin/bash

# Script to start a 3-node sharded RAFT cluster

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}Starting Titan-KV Sharded Cluster (3 nodes)...${NC}"

# Create data directories and logs
mkdir -p data/node1 data/node2 data/node3 logs

# Kill any existing processes on these ports
echo -e "${YELLOW}Cleaning up existing processes...${NC}"
pkill -f "cmd/server/main.go" || true
pkill -f "raft-addr.*:12000" || true
pkill -f "raft-addr.*:12001" || true
pkill -f "raft-addr.*:12002" || true
sleep 2

# Define peer list for each node (all nodes know about each other)
# Format: nodeID|grpcAddr|raftAddr
PEERS="node2|localhost:50052|localhost:12001,node3|localhost:50053|localhost:12002"

# Start Node 1 (Bootstrap)
echo -e "${GREEN}Starting Node 1 (Bootstrap Leader)...${NC}"
go run cmd/server/main.go \
  -node-id node1 \
  -grpc-addr localhost:50051 \
  -raft-addr localhost:12000 \
  -data-dir ./data/node1 \
  -bootstrap \
  -peers "$PEERS" \
  > logs/node1.log 2>&1 &
NODE1_PID=$!

sleep 3

# Start Node 2
echo -e "${GREEN}Starting Node 2...${NC}"
PEERS_NODE2="node1|localhost:50051|localhost:12000,node3|localhost:50053|localhost:12002"
go run cmd/server/main.go \
  -node-id node2 \
  -grpc-addr localhost:50052 \
  -raft-addr localhost:12001 \
  -data-dir ./data/node2 \
  -peers "$PEERS_NODE2" \
  > logs/node2.log 2>&1 &
NODE2_PID=$!

sleep 2

# Start Node 3
echo -e "${GREEN}Starting Node 3...${NC}"
PEERS_NODE3="node1|localhost:50051|localhost:12000,node2|localhost:50052|localhost:12001"
go run cmd/server/main.go \
  -node-id node3 \
  -grpc-addr localhost:50053 \
  -raft-addr localhost:12002 \
  -data-dir ./data/node3 \
  -peers "$PEERS_NODE3" \
  > logs/node3.log 2>&1 &
NODE3_PID=$!

sleep 3

echo -e "${BLUE}Sharded cluster started!${NC}"
echo ""
echo "Node 1: gRPC localhost:50051, RAFT localhost:12000 (PID: $NODE1_PID)"
echo "Node 2: gRPC localhost:50052, RAFT localhost:12001 (PID: $NODE2_PID)"
echo "Node 3: gRPC localhost:50053, RAFT localhost:12002 (PID: $NODE3_PID)"
echo ""
echo "Logs are in logs/node*.log"
echo ""
echo "Test the sharded cluster:"
echo "  go run cmd/cli/main.go -address localhost:50051"
echo ""
echo "Keys will be automatically sharded across nodes based on consistent hashing"
echo ""
echo "To stop the cluster, run: ./scripts/stop-cluster.sh"
echo "Or press Ctrl+C"

# Wait for interrupt
trap "echo ''; echo 'Stopping cluster...'; kill $NODE1_PID $NODE2_PID $NODE3_PID 2>/dev/null; sleep 1; exit" INT TERM

wait

