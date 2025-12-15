# Titan-KV: Distributed Key-Value Store

A production-grade distributed key-value store implementation in Go, demonstrating fundamental distributed systems principles through a multi-phase architecture: single-node storage engine, network communication layer, distributed consensus, and horizontal scalability.

## Overview

Titan-KV implements a CP (Consistency + Partition-tolerance) distributed system using the Raft consensus algorithm for strong consistency guarantees. The system features:

- **Bitcask-inspired storage engine**: Append-only log with in-memory index for O(1) reads and writes
- **Raft consensus**: Distributed replication with automatic leader election and fault tolerance
- **Consistent hashing**: Horizontal scalability through sharding with minimal data movement
- **gRPC communication**: Efficient binary protocol for inter-node and client-server communication

For detailed architectural decisions and design trade-offs, see [DESIGN_GUIDE.md](./DESIGN_GUIDE.md).

## Architecture

Titan-KV follows a layered architecture:

```
┌─────────────────────────────────────────┐
│         Client Applications             │
└─────────────────┬───────────────────────┘
                  │ gRPC
┌─────────────────▼───────────────────────┐
│      Sharding & Request Routing         │
│    (Consistent Hashing Ring)            │
└─────────────────┬───────────────────────┘
                  │
┌─────────────────▼───────────────────────┐
│      Raft Consensus Layer               │
│    (Leader Election & Log Replication)  │
└─────────────────┬───────────────────────┘
                  │
┌─────────────────▼───────────────────────┐
│      Storage Engine (Bitcask)           │
│    (Append-Only Log + In-Memory Index)   │
└─────────────────────────────────────────┘
```

## Features

### Phase 1: Core Storage Engine

**Status**: Complete

Implements a Bitcask-inspired storage engine with the following characteristics:

- **Append-only log**: All writes are sequential appends to disk for durability
- **In-memory keyDir**: Hash map mapping keys to record locations (file ID, offset, size)
- **Garbage collection**: Periodic compaction merges data files and reclaims disk space
- **Durability guarantees**: PUT, GET, and DELETE operations with fsync for strong durability

**Key Components**:
- `storage/bitcask.go`: Core storage engine implementation
- `storage/compaction.go`: Log compaction and garbage collection
- `storage/api.go`: High-level storage API

**Usage**:
```bash
go run cmd/kv/main.go -data-dir ./data
```

### Phase 2: Network Communication Layer

**Status**: Complete

Adds gRPC-based network communication using Protocol Buffers:

- **Protocol Buffers**: Binary serialization for efficient network communication
- **gRPC server**: Exposes storage engine APIs over the network
- **gRPC client library**: Client SDK with timeout support and connection management
- **CLI client**: Interactive command-line interface for testing

**Key Components**:
- `proto/kvstore.proto`: Protocol Buffer schema definition
- `server/server.go`: gRPC server implementation
- `client/client.go`: gRPC client library
- `cmd/cli/main.go`: CLI client tool

**Usage**:
```bash
# Start server
go run cmd/server/main.go -address :50051 -data-dir ./data

# Connect with CLI client
go run cmd/cli/main.go -address localhost:50051
```

**CLI Commands**:
- `PUT <key> <value>`: Store a key-value pair
- `GET <key>`: Retrieve a value by key
- `DELETE <key>`: Delete a key-value pair
- `QUIT`: Exit the client

### Phase 3: Distributed Consensus and Replication

**Status**: Complete

Integrates Raft consensus algorithm for distributed replication and fault tolerance:

- **Raft consensus**: Leader election, log replication, and safety guarantees
- **State machine replication**: All write operations committed via Raft before applying to storage
- **Fault tolerance**: System tolerates (N-1)/2 node failures
- **Automatic recovery**: Leader election and log replication handle node failures

**Key Components**:
- `raft/fsm.go`: Finite State Machine implementing `raft.FSM` interface
- `raft/node.go`: Raft node wrapper and cluster management
- `raft/join.go`: Cluster membership management
- `server/raft_server.go`: gRPC server with Raft integration

**Usage**:
```bash
# Start bootstrap node
go run cmd/server/main.go \
  -node-id node1 \
  -grpc-addr :50051 \
  -raft-addr :12000 \
  -data-dir ./data/node1 \
  -bootstrap

# Start additional nodes
go run cmd/server/main.go \
  -node-id node2 \
  -grpc-addr :50052 \
  -raft-addr :12001 \
  -data-dir ./data/node2
```

**Cluster Behavior**:
- Write operations (PUT/DELETE) must go through the leader
- Read operations (GET) can be served by any node
- Automatic leader election on leader failure
- Data replication across all nodes via Raft log

### Phase 4: Horizontal Scalability (Sharding)

**Status**: Complete

Implements sharding using consistent hashing for horizontal scalability:

- **Consistent hashing ring**: SHA-256-based hash ring with virtual nodes (150 per physical node)
- **Decentralized routing**: Any node can receive requests and route to the correct shard
- **Request forwarding**: Automatic forwarding when request arrives at wrong shard
- **Read repair**: Background process repairs inconsistencies across replicas

**Key Components**:
- `sharding/ring.go`: Consistent hashing ring implementation
- `sharding/membership.go`: Cluster membership service
- `server/sharded_server.go`: Sharded gRPC server with request routing

**Usage**:
```bash
# Start sharded cluster
./scripts/start-sharded-cluster.sh

# Or manually start nodes with peer configuration
go run cmd/server/main.go \
  -node-id node1 \
  -grpc-addr localhost:50051 \
  -raft-addr localhost:12000 \
  -data-dir ./data/node1 \
  -bootstrap \
  -peers "node2|localhost:50052|localhost:12001,node3|localhost:50053|localhost:12002"
```

**Sharding Behavior**:
1. Key hash determines target shard using consistent hashing ring
2. Request routed to target shard (local processing or forwarding)
3. Write operations committed via Raft within the shard
4. Read operations served from local storage with optional read repair

## Project Structure

```
titan-distributed-kv/
├── storage/              # Core storage engine
│   ├── bitcask.go       # Bitcask implementation
│   ├── compaction.go    # Garbage collection
│   └── api.go           # High-level API
├── proto/                # Protocol Buffers
│   ├── kvstore.proto    # Protobuf schema
│   └── kvstore/          # Generated code
│       └── kvstore.pb.go
├── server/               # gRPC server implementations
│   ├── server.go        # Basic gRPC server
│   ├── raft_server.go   # Raft-enabled server
│   └── sharded_server.go # Sharded server
├── client/               # gRPC client library
│   └── client.go
├── raft/                 # Raft consensus layer
│   ├── fsm.go           # Finite State Machine
│   ├── node.go          # Raft node wrapper
│   └── join.go          # Cluster membership
├── sharding/             # Sharding layer
│   ├── ring.go          # Consistent hashing ring
│   └── membership.go    # Cluster membership service
├── cmd/
│   ├── kv/              # Example program
│   │   └── main.go
│   ├── server/          # Server executable
│   │   └── main.go
│   └── cli/             # CLI client
│       └── main.go
├── scripts/              # Cluster management scripts
│   ├── start-cluster.sh
│   ├── start-sharded-cluster.sh
│   └── stop-cluster.sh
├── tests/                # Integration tests
│   └── integration_test.go
├── doc/
│   └── Prompt-log.md    # Project plan
├── DESIGN_GUIDE.md      # Architectural deep dive
├── Makefile             # Build automation
└── README.md
```

## Requirements

- Go 1.21 or later
- Protocol Buffer compiler (`protoc`) - optional, generated code included
- gRPC Go plugins - optional, generated code included

Dependencies are managed via `go.mod`:
- `github.com/hashicorp/raft`: Raft consensus library
- `github.com/hashicorp/raft-boltdb/v2`: Persistent Raft log storage
- `google.golang.org/grpc`: gRPC framework
- `google.golang.org/protobuf`: Protocol Buffers

## Building

Install dependencies:
```bash
go mod download
go mod tidy
```

Build binaries:
```bash
make build        # Build all binaries
make server       # Build server only
make cli          # Build CLI client only
```

Regenerate Protocol Buffer code (if schema changes):
```bash
make proto
```

Binaries are created in the `bin/` directory:
- `bin/server`: gRPC server executable
- `bin/cli`: CLI client executable

## Testing

Run integration tests:
```bash
go test ./tests/... -v
```

Integration tests verify:
- Basic write and read operations with replication
- Leader failure and data persistence
- Multiple writes across cluster nodes

## System Characteristics

### Consistency Model

Titan-KV provides **linearizable consistency**:
- All write operations are committed via Raft consensus
- Reads may be served from any node (may see slightly stale data)
- Strong consistency guarantees for writes

### CAP Theorem

Titan-KV is a **CP system** (Consistency + Partition-tolerance):
- Prioritizes consistency over availability during network partitions
- Minority partitions become unavailable to prevent split-brain scenarios
- Majority partitions continue operating normally

### Fault Tolerance

- Tolerates (N-1)/2 node failures in a cluster of N nodes
- Automatic leader election on leader failure
- Data replication across all nodes via Raft log
- Persistent storage ensures data durability

### Scalability

- Horizontal scalability through sharding
- Consistent hashing minimizes data movement on node addition/removal
- Each shard operates independently with its own Raft cluster
- Virtual nodes ensure even load distribution

## Documentation

- [DESIGN_GUIDE.md](./DESIGN_GUIDE.md): Comprehensive architectural guide covering:
  - Raft consensus algorithm deep dive
  - Storage engine internals and trade-offs
  - Consistent hashing and sharding strategies
  - Design trade-offs and system characteristics
  - Code map and implementation details

## License

MIT License
