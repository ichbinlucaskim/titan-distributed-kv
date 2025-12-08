#!/bin/bash

# Script to stop all cluster nodes

echo "Stopping Titan-KV cluster nodes..."

pkill -f "cmd/server/main.go" || true
pkill -f "raft-addr.*:12000" || true
pkill -f "raft-addr.*:12001" || true
pkill -f "raft-addr.*:12002" || true

sleep 1

echo "Cluster stopped."

