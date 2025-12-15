# Titan-KV: Deep Dive Technical Study Guide

## Table of Contents

1. [Introduction](#introduction)
2. [Distributed Consensus: Raft Algorithm](#distributed-consensus-raft-algorithm)
3. [Storage Engine: Bitcask Architecture](#storage-engine-bitcask-architecture)
4. [Scalability: Sharding and Consistent Hashing](#scalability-sharding-and-consistent-hashing)
5. [Networking: gRPC and Protocol Buffers](#networking-grpc-and-protocol-buffers)
6. [Design Trade-offs and System Characteristics](#design-trade-offs-and-system-characteristics)
7. [Code Map: Implementation Details](#code-map-implementation-details)

---

## Introduction

Titan-KV is a distributed key-value store that demonstrates fundamental distributed systems principles through a production-grade implementation. This guide bridges the gap between theoretical concepts and practical implementation, explaining the architectural decisions and trade-offs that shape the system.

### System Overview

Titan-KV implements a multi-layered architecture:

1. **Storage Layer**: Bitcask-inspired append-only log storage engine
2. **Consensus Layer**: Raft algorithm for distributed consensus and replication
3. **Sharding Layer**: Consistent hashing for horizontal scalability
4. **Network Layer**: gRPC with Protocol Buffers for efficient RPC communication

The system prioritizes **strong consistency** (CP in CAP theorem) while maintaining high availability through replication and fault tolerance.

---

## Distributed Consensus: Raft Algorithm

### Why Raft Instead of Paxos?

Raft was chosen over Paxos for several critical reasons:

1. **Understandability**: Raft separates leader election, log replication, and safety into distinct sub-problems, making it easier to reason about and implement correctly.

2. **Operational Simplicity**: Raft provides stronger guarantees about cluster membership changes and makes it easier to add/remove nodes safely.

3. **Production Readiness**: The HashiCorp Raft library (`hashicorp/raft`) is battle-tested in production systems like Consul and Vault.

**The Naive Approach**: A simple master-slave replication would fail because:
- No guarantee of consistency across replicas
- Split-brain scenarios during network partitions
- Data loss during master failures
- No way to detect which replica has the "correct" data

### Raft Fundamentals

Raft ensures that all nodes in a cluster agree on a sequence of state machine commands through three mechanisms:

#### 1. Leader Election

**Problem**: In a distributed system, nodes must agree on who can accept writes. Without a leader, concurrent writes could create inconsistencies.

**Solution**: Raft uses a randomized timeout mechanism where:
- Each node starts as a **Follower**
- If no heartbeat is received from a leader within an election timeout, the follower becomes a **Candidate**
- Candidates request votes from other nodes
- A candidate becomes **Leader** if it receives votes from a majority of nodes

**Code Reference**: `raft/node.go:NewNode()` initializes the Raft node with `raft.DefaultConfig()`, which includes election timeout settings.

```go
raftConfig := raft.DefaultConfig()
raftConfig.LocalID = raft.ServerID(config.NodeID)
raftConfig.SnapshotInterval = 30 * time.Second
raftConfig.SnapshotThreshold = 2
```

**What Happens During Network Partition?**

Consider a 5-node cluster split into two partitions: [A, B, C] and [D, E].

- **Partition 1 (3 nodes)**: Can form a quorum and elect a leader. Writes succeed.
- **Partition 2 (2 nodes)**: Cannot form a quorum (needs 3/5 = majority). No leader elected. Writes fail.

This is the **CP (Consistency + Partition-tolerance)** trade-off: During a partition, Titan-KV prioritizes consistency over availability. The minority partition becomes unavailable to prevent split-brain scenarios.

#### 2. Log Replication

**Problem**: Once a leader is elected, how do we ensure all nodes have the same sequence of operations?

**Solution**: Raft uses a replicated log where:
- The leader accepts client requests and appends them to its log
- The leader replicates log entries to followers
- An entry is **committed** (applied to state machine) only after a majority of nodes have replicated it

**Code Reference**: `raft/node.go:ApplyLogEntry()` demonstrates this:

```go
func (n *Node) ApplyLogEntry(entry LogEntry) error {
    if n.raft.State() != raft.Leader {
        return fmt.Errorf("not the leader")
    }
    
    data, err := json.Marshal(entry)
    future := n.raft.Apply(data, 10*time.Second)
    return future.Error()
}
```

**Safety Guarantee**: Raft ensures that if two different servers have the same log entry at the same index, they have the same term and the same command. This is the **Log Matching Property**.

#### 3. Safety Properties

Raft guarantees three critical safety properties:

1. **Election Safety**: At most one leader can be elected in a given term
2. **Leader Append-Only**: A leader never overwrites or deletes entries in its log
3. **State Machine Safety**: If a server has applied a log entry at a given index, no other server will apply a different log entry at the same index

**Code Reference**: `raft/fsm.go:Apply()` shows how log entries are applied to the state machine:

```go
func (f *FSM) Apply(log *raft.Log) interface{} {
    var entry LogEntry
    json.Unmarshal(log.Data, &entry)
    
    switch entry.Operation {
    case "PUT":
        return f.api.Put(entry.Key, entry.Value)
    case "DELETE":
        return f.api.Delete(entry.Key)
    }
}
```

### State Machine Replication

The Finite State Machine (FSM) pattern ensures that all nodes apply the same sequence of operations in the same order, resulting in identical state.

**Code Map**:
- `raft/fsm.go`: Implements `raft.FSM` interface
- `raft/node.go`: Wraps HashiCorp Raft library
- `server/raft_server.go`: Integrates Raft with gRPC server

**Read Operations**: Reads are served directly from local storage (`server/raft_server.go:Get()`) without going through Raft. This provides low latency but requires careful consideration of consistency guarantees.

---

## Storage Engine: Bitcask Architecture

### Why Bitcask?

Bitcask was chosen over LSM-Trees (Log-Structured Merge Trees) for its simplicity and predictable performance characteristics:

**Advantages**:
- **O(1) writes**: All writes are sequential appends to a log file
- **O(1) reads**: In-memory hash map provides direct key-to-location mapping
- **Crash recovery**: Simple recovery by replaying log files
- **Durability**: Each write can be fsync'd for strong durability guarantees

**Limitations**:
- **Memory bound**: The keyDir (hash map) must fit in RAM
- **Compaction overhead**: Periodic compaction merges multiple files
- **Not optimized for range queries**: Designed for point lookups

### Append-Only Log Structure

**The Core Idea**: Instead of updating data in place, all writes are appended to a log file. This provides:

1. **Write Performance**: Sequential disk writes are orders of magnitude faster than random writes
2. **Durability**: Append-only writes are atomic (either complete or not)
3. **Crash Recovery**: On restart, replay the log to rebuild state

**Code Reference**: `storage/bitcask.go:Put()`:

```go
func (bc *Bitcask) Put(key, value []byte) error {
    // Write record to log file
    binary.Write(bc.currentFile, binary.LittleEndian, uint32(len(key)))
    binary.Write(bc.currentFile, binary.LittleEndian, uint32(len(value)))
    bc.currentFile.Write(key)
    bc.currentFile.Write(value)
    
    // Ensure durability
    bc.currentFile.Sync()
    
    // Update in-memory index
    bc.keyDir[string(key)] = &RecordLocation{
        FileID: bc.currentFileID,
        Offset: offset,
        Size:   recordSize,
    }
}
```

**Record Format**: Each record contains:
- Key size (4 bytes)
- Value size (4 bytes)
- Key (variable length)
- Value (variable length)

### In-Memory KeyDir

The `keyDir` is a hash map that maps keys to their location in log files:

```go
type RecordLocation struct {
    FileID uint32  // Which log file contains this key
    Offset int64   // Byte offset within the file
    Size   int64   // Size of the record
}
```

**Why Not Store Values in Memory?**

Storing only locations (not values) provides:
- **Memory efficiency**: Only metadata stored in RAM
- **Large value support**: Values can be arbitrarily large without consuming RAM
- **Fast lookups**: O(1) hash map lookup, then single disk seek

**Trade-off**: This design requires that the keyDir fits in memory. For systems with billions of keys, this becomes a limitation. Alternatives like LSM-Trees or B-Trees would be more appropriate.

### Garbage Collection and Compaction

**Problem**: As keys are updated or deleted, old records accumulate in log files, wasting disk space.

**Solution**: Periodic compaction merges multiple log files, keeping only the latest version of each key.

**Code Reference**: `storage/compaction.go:Compact()`:

```go
func (c *Compactor) Compact() error {
    // Create a new merged file
    mergedFileID := maxFileID + 1
    
    // Write all active keys to the merged file
    for key, location := range c.bc.keyDir {
        // Read record from old file
        // Write to merged file
        // Update keyDir with new location
    }
    
    // Delete old files
    // Update Bitcask state
}
```

**Compaction Process**:
1. Lock the storage engine (prevents concurrent writes)
2. Create a new merged file
3. Iterate through `keyDir` (which contains only active keys)
4. Copy each active record to the merged file
5. Update `keyDir` with new file locations
6. Delete old log files
7. Replace current file with merged file

**Why This Works**: The `keyDir` only contains the latest version of each key. By iterating through `keyDir`, we automatically skip deleted and overwritten keys.

**Tombstone Handling**: Deletes are marked with a special value size (`0xFFFFFFFF`). During compaction, deleted keys are simply not included in the merged file.

---

## Scalability: Sharding and Consistent Hashing

### The Sharding Problem

**Naive Approach**: `hash(key) % N` where N is the number of nodes.

**Why This Fails**:
- **Rebalancing Nightmare**: When N changes (node added/removed), most keys need to be moved
- **Uneven Distribution**: Hash modulo can create hotspots if hash distribution is poor
- **No Virtual Nodes**: Cannot balance load across nodes with different capacities

**Example**: With 3 nodes, key "user:123" maps to node `hash("user:123") % 3 = 1`. If we add a 4th node, it now maps to `hash("user:123") % 4 = 3`. The key must be moved from node 1 to node 3.

### Consistent Hashing Solution

Consistent hashing maps both keys and nodes onto a hash ring. Each key is assigned to the first node encountered when moving clockwise around the ring.

**Key Properties**:
1. **Minimal Rebalancing**: When a node is added/removed, only keys adjacent to that node need to be moved
2. **Load Distribution**: Virtual nodes ensure even key distribution
3. **Fault Tolerance**: Node failures only affect keys assigned to that node

**Code Reference**: `sharding/ring.go:GetNode()`:

```go
func (r *HashRing) GetNode(key string) (*NodeInfo, error) {
    keyHash := r.hashKey(key)
    
    // Binary search for first node with hash >= keyHash
    idx := sort.Search(len(r.sortedKeys), func(i int) bool {
        return r.sortedKeys[i] >= keyHash
    })
    
    // Wrap around if at end of ring
    if idx >= len(r.sortedKeys) {
        idx = 0
    }
    
    // Find which node owns this hash
    targetHash := r.sortedKeys[idx]
    // ... return node info
}
```

### Virtual Nodes

**Problem**: With only physical nodes on the ring, load distribution can be uneven, especially with small cluster sizes.

**Solution**: Each physical node is represented by multiple "virtual nodes" (replicas) on the ring. Titan-KV uses 150 virtual nodes per physical node by default.

**Code Reference**: `sharding/ring.go:rebuildSortedKeys()`:

```go
func (r *HashRing) rebuildSortedKeys() {
    r.sortedKeys = make([]uint32, 0, len(r.nodes)*r.replicas)
    for nodeID := range r.nodes {
        for i := 0; i < r.replicas; i++ {
            hash := r.hashNode(nodeID, i)
            r.sortedKeys = append(r.sortedKeys, hash)
        }
    }
    sort.Slice(r.sortedKeys, func(i, j int) bool {
        return r.sortedKeys[i] < r.sortedKeys[j]
    })
}
```

**Benefits**:
- **Better Load Balancing**: More points on the ring = more even distribution
- **Capacity Awareness**: Can assign more virtual nodes to higher-capacity nodes
- **Smoother Rebalancing**: Virtual nodes spread rebalancing across multiple physical nodes

### Request Routing

**Decentralized Routing**: Any node can receive a client request. The receiving node:
1. Computes the hash of the key
2. Determines the target shard using the hash ring
3. If local: processes the request
4. If remote: forwards the request to the target node

**Code Reference**: `server/sharded_server.go:Put()`:

```go
func (s *ShardedKVServer) Put(ctx context.Context, req *kvstore.PutRequest) (*kvstore.PutResponse, error) {
    // Determine target shard
    targetNode, err := s.membership.GetTargetNode(req.Key)
    
    // If not local, forward
    if !s.membership.IsLocalNode(targetNode.NodeID) {
        return s.forwardPut(ctx, targetNode, req)
    }
    
    // Local: check if leader, then apply via Raft
    if !s.node.IsLeader() {
        return &kvstore.PutResponse{
            Error: fmt.Sprintf("not the leader, redirect to: %s", s.node.Leader()),
        }, nil
    }
    
    // Apply through Raft consensus
    return s.applyPut(req)
}
```

**Why This Design?**
- **Client Simplicity**: Clients don't need to know shard locations
- **Load Distribution**: Requests naturally distribute across nodes
- **Fault Tolerance**: If a node fails, requests can be forwarded to replicas

### Read Repair

**Problem**: In a distributed system with replication, nodes can temporarily have inconsistent data due to network issues, node failures, or concurrent updates.

**Solution**: Read repair checks replicas after a successful read and repairs any inconsistencies.

**Code Reference**: `server/sharded_server.go:performReadRepair()`:

```go
func (s *ShardedKVServer) performReadRepair(key string, localValue []byte) {
    replicas, _ := s.membership.GetReplicas(key, s.replicationFactor)
    
    for _, replica := range replicas {
        replicaValue, err := cli.Get(nil, key)
        if err != nil {
            // Replica missing key - repair it
            cli.Put(nil, key, localValue)
        } else if !equalBytes(localValue, replicaValue) {
            // Value mismatch - log conflict
            log.Printf("Read repair: value mismatch for key %s", key)
        }
    }
}
```

**Trade-off**: Read repair adds latency to read operations. It's performed asynchronously to minimize impact on read latency.

---

## Networking: gRPC and Protocol Buffers

### Why Protocol Buffers Over JSON?

**Performance**:
- **Binary Encoding**: Protocol Buffers use binary encoding, resulting in smaller message sizes (typically 3-10x smaller than JSON)
- **Faster Serialization**: Binary encoding/decoding is faster than text parsing
- **Schema Evolution**: Backward and forward compatibility through versioned schemas

**Code Reference**: `proto/kvstore.proto`:

```protobuf
message PutRequest {
  string key = 1;
  bytes value = 2;
}

message PutResponse {
  bool success = 1;
  string error = 2;
}
```

**Schema Benefits**:
- **Type Safety**: Compile-time type checking
- **Code Generation**: Automatic client/server code generation
- **Documentation**: Schema serves as API documentation

### gRPC Advantages

**HTTP/2 Based**:
- **Multiplexing**: Multiple requests over a single connection
- **Header Compression**: Reduces overhead
- **Streaming**: Supports bidirectional streaming

**Code Reference**: `client/client.go`:

```go
func NewClient(address string) (*Client, error) {
    conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
    return &Client{
        conn:   conn,
        client: kvstore.NewKeyValueStoreClient(conn),
    }, nil
}
```

**Connection Management**: gRPC maintains persistent connections, reducing connection overhead compared to HTTP/1.1.

### Request Routing Flow

```
Client Request
    ↓
Random Node (gRPC Server)
    ↓
Compute Key Hash → Determine Target Shard
    ↓
Is Local Shard?
    ├─ Yes → Is Leader?
    │         ├─ Yes → Apply via Raft → Return Success
    │         └─ No → Return Leader Redirect
    └─ No → Forward to Target Node → Return Response
```

**Code Map**:
- `server/sharded_server.go`: Request routing logic
- `sharding/membership.go`: Hash ring and node management
- `client/client.go`: gRPC client implementation

---

## Design Trade-offs and System Characteristics

### CAP Theorem Analysis

**Titan-KV's Position: CP (Consistency + Partition-tolerance)**

**Consistency**: Strong consistency through Raft consensus. All reads see the latest committed write.

**Partition-tolerance**: System continues operating during network partitions, but minority partitions become unavailable.

**Availability Trade-off**: During a partition, if a majority of nodes are unavailable, the system becomes unavailable for writes. This is intentional to prevent split-brain scenarios.

**When to Choose CP**:
- Financial systems (cannot tolerate inconsistencies)
- Configuration management (consistency critical)
- Leader election systems (must have single source of truth)

**When to Choose AP**:
- Social media feeds (eventual consistency acceptable)
- DNS systems (availability more important)
- Caching layers (stale data acceptable)

### Consistency Models

**Titan-KV's Consistency Model: Linearizability**

**Definition**: A system is linearizable if, for every operation, there exists a point in time between the operation's invocation and response where the operation appears to have taken effect atomically.

**How Titan-KV Achieves This**:
1. All writes go through Raft leader
2. Writes are committed only after majority replication
3. Reads can be served from any node (but may see slightly stale data)

**Read Consistency Options**:
- **Current Implementation**: Reads from local storage (may be slightly stale)
- **Strong Consistency**: Reads could go through Raft to ensure latest committed value
- **Bounded Staleness**: Track replication lag and reject reads if too stale

**Trade-off**: Stronger read consistency increases latency and reduces availability.

### Memory vs Disk Trade-offs

**Current Design: Memory-Efficient**

**Memory Usage**:
- KeyDir: O(K) where K = number of keys
- Values: Stored on disk, not in memory

**Disk Usage**:
- Append-only log: O(W) where W = total bytes written (includes overwrites)
- Compaction reduces disk usage by removing old versions

**Alternative: In-Memory Database**

**Pros**:
- Faster reads (no disk I/O)
- Simpler implementation (no compaction needed)

**Cons**:
- Limited by RAM size
- Data loss on power failure (unless replicated)
- Higher cost per GB

**When to Choose Each**:
- **Bitcask (Current)**: Large values, write-heavy workloads, cost-sensitive
- **In-Memory**: Small datasets, read-heavy workloads, latency-sensitive

### Write Durability Guarantees

**Current Implementation: Strong Durability**

Each write operation calls `fsync()` to ensure data is written to persistent storage:

```go
func (bc *Bitcask) Put(key, value []byte) error {
    // ... write to file ...
    bc.currentFile.Sync()  // Force write to disk
    // ... update keyDir ...
}
```

**Trade-off**: `fsync()` is expensive (typically 1-10ms). For high-throughput systems, batching fsyncs can improve performance at the cost of durability.

**Durability Levels**:
1. **No fsync**: Fastest, but data loss on power failure
2. **fsync per write**: Strongest durability, slower
3. **Periodic fsync**: Balance between performance and durability

### Scalability Limits

**Current Limitations**:

1. **KeyDir Memory Bound**: System limited by RAM for key count
   - **Mitigation**: Use LSM-Trees for larger key spaces

2. **Single Raft Cluster Per Shard**: Each shard has its own Raft cluster
   - **Implication**: Shard size limited by Raft performance (typically 5-7 nodes per shard)

3. **No Cross-Shard Transactions**: Operations cannot span multiple shards atomically
   - **Mitigation**: Application-level coordination or two-phase commit

4. **Compaction Overhead**: Compaction locks the storage engine
   - **Mitigation**: Incremental compaction or LSM-Tree merge strategy

**Horizontal Scaling Strategy**:
- Add more shards (nodes) to increase capacity
- Each shard independently scales its Raft cluster
- Consistent hashing minimizes data movement

---

## Code Map: Implementation Details

### Storage Layer

**Files**:
- `storage/bitcask.go`: Core storage engine implementation
- `storage/compaction.go`: Garbage collection and log compaction
- `storage/api.go`: High-level API interface

**Key Functions**:
- `Bitcask.Put()`: Append record to log file, update keyDir
- `Bitcask.Get()`: Lookup key in keyDir, read from log file
- `Bitcask.Delete()`: Write tombstone record, remove from keyDir
- `Compactor.Compact()`: Merge log files, reclaim disk space

### Consensus Layer

**Files**:
- `raft/fsm.go`: Finite State Machine implementing `raft.FSM`
- `raft/node.go`: Raft node wrapper and cluster management
- `raft/join.go`: Cluster membership management

**Key Functions**:
- `Node.ApplyLogEntry()`: Apply log entry through Raft consensus
- `FSM.Apply()`: Apply log entry to storage engine
- `Node.AddPeer()`: Add node to Raft cluster

### Sharding Layer

**Files**:
- `sharding/ring.go`: Consistent hashing ring implementation
- `sharding/membership.go`: Cluster membership service

**Key Functions**:
- `HashRing.GetNode()`: Find node responsible for a key
- `HashRing.GetReplicas()`: Get replica nodes for a key
- `MembershipService.GetTargetNode()`: Determine target shard for routing

### Network Layer

**Files**:
- `server/sharded_server.go`: Sharded gRPC server with request routing
- `server/raft_server.go`: Raft-enabled gRPC server
- `client/client.go`: gRPC client library

**Key Functions**:
- `ShardedKVServer.Put()`: Route PUT request to correct shard
- `ShardedKVServer.Get()`: Route GET request, perform read repair
- `ShardedKVServer.forwardPut()`: Forward request to remote shard

---

## Conclusion

Titan-KV demonstrates how fundamental distributed systems principles translate into production code. The system makes deliberate trade-offs:

- **CP over AP**: Prioritizes consistency over availability
- **Memory efficiency**: Stores only metadata in RAM
- **Simplicity**: Bitcask over LSM-Trees for clarity
- **Strong durability**: fsync per write for data safety

Understanding these trade-offs is crucial for system design interviews and real-world distributed systems engineering. Each architectural decision reflects a balance between performance, consistency, availability, and operational complexity.

