package server

import (
	"context"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"titan-kv/proto/kvstore"
	"titan-kv/raft"
	"titan-kv/sharding"
	"titan-kv/storage"
)

// ShardedKVServer implements the KeyValueStore gRPC service with sharding support
type ShardedKVServer struct {
	kvstore.UnimplementedKeyValueStoreServer
	api            *storage.API
	node           *raft.Node
	membership     *sharding.MembershipService
	readRepair     bool // Enable read repair
	replicationFactor int // Number of replicas for reads
}

// NewShardedKVServer creates a new sharded gRPC server instance
func NewShardedKVServer(api *storage.API, node *raft.Node, membership *sharding.MembershipService) *ShardedKVServer {
	return &ShardedKVServer{
		api:               api,
		node:              node,
		membership:        membership,
		readRepair:        true,
		replicationFactor: 2, // Read from 2 replicas for read repair
	}
}

// Put implements the Put RPC method with sharding
func (s *ShardedKVServer) Put(ctx context.Context, req *kvstore.PutRequest) (*kvstore.PutResponse, error) {
	if req.Key == "" {
		return &kvstore.PutResponse{
			Success: false,
			Error:   "key cannot be empty",
		}, nil
	}

	// Determine target shard
	targetNode, err := s.membership.GetTargetNode(req.Key)
	if err != nil {
		return &kvstore.PutResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to determine target shard: %v", err),
		}, nil
	}

	// If this is not the target shard, forward the request
	if !s.membership.IsLocalNode(targetNode.NodeID) {
		return s.forwardPut(ctx, targetNode, req)
	}

	// This is the target shard - check if we're the leader
	if !s.node.IsLeader() {
		leader := s.node.Leader()
		if leader == "" {
			return &kvstore.PutResponse{
				Success: false,
				Error:   "no leader available",
			}, nil
		}
		return &kvstore.PutResponse{
			Success: false,
			Error:   fmt.Sprintf("not the leader, redirect to: %s", leader),
		}, nil
	}

	// Apply through RAFT consensus
	entry := raft.LogEntry{
		Operation: "PUT",
		Key:       req.Key,
		Value:     req.Value,
	}

	if err := s.node.ApplyLogEntry(entry); err != nil {
		return &kvstore.PutResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to apply: %v", err),
		}, nil
	}

	return &kvstore.PutResponse{
		Success: true,
	}, nil
}

// Get implements the Get RPC method with sharding and read repair
func (s *ShardedKVServer) Get(ctx context.Context, req *kvstore.GetRequest) (*kvstore.GetResponse, error) {
	if req.Key == "" {
		return &kvstore.GetResponse{
			Found: false,
			Error: "key cannot be empty",
		}, nil
	}

	// Determine target shard
	targetNode, err := s.membership.GetTargetNode(req.Key)
	if err != nil {
		return &kvstore.GetResponse{
			Found: false,
			Error: fmt.Sprintf("failed to determine target shard: %v", err),
		}, nil
	}

	// If this is not the target shard, forward the request
	if !s.membership.IsLocalNode(targetNode.NodeID) {
		return s.forwardGet(ctx, targetNode, req)
	}

	// This is the target shard - serve directly from local storage
	value, err := s.api.Get(req.Key)
	if err != nil {
		if err.Error() == "get operation failed: key not found" {
			return &kvstore.GetResponse{
				Found: false,
			}, nil
		}
		return &kvstore.GetResponse{
			Found: false,
			Error: err.Error(),
		}, nil
	}

	// Read repair: check replicas if enabled
	if s.readRepair {
		go s.performReadRepair(req.Key, value)
	}

	return &kvstore.GetResponse{
		Found: true,
		Value: value,
	}, nil
}

// Delete implements the Delete RPC method with sharding
func (s *ShardedKVServer) Delete(ctx context.Context, req *kvstore.DeleteRequest) (*kvstore.DeleteResponse, error) {
	if req.Key == "" {
		return &kvstore.DeleteResponse{
			Success: false,
			Error:   "key cannot be empty",
		}, nil
	}

	// Determine target shard
	targetNode, err := s.membership.GetTargetNode(req.Key)
	if err != nil {
		return &kvstore.DeleteResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to determine target shard: %v", err),
		}, nil
	}

	// If this is not the target shard, forward the request
	if !s.membership.IsLocalNode(targetNode.NodeID) {
		return s.forwardDelete(ctx, targetNode, req)
	}

	// This is the target shard - check if we're the leader
	if !s.node.IsLeader() {
		leader := s.node.Leader()
		if leader == "" {
			return &kvstore.DeleteResponse{
				Success: false,
				Error:   "no leader available",
			}, nil
		}
		return &kvstore.DeleteResponse{
			Success: false,
			Error:   fmt.Sprintf("not the leader, redirect to: %s", leader),
		}, nil
	}

	// Apply through RAFT consensus
	entry := raft.LogEntry{
		Operation: "DELETE",
		Key:       req.Key,
		Value:     nil,
	}

	if err := s.node.ApplyLogEntry(entry); err != nil {
		return &kvstore.DeleteResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to apply: %v", err),
		}, nil
	}

	return &kvstore.DeleteResponse{
		Success: true,
	}, nil
}

// forwardPut forwards a PUT request to the target node
func (s *ShardedKVServer) forwardPut(ctx context.Context, targetNode *sharding.NodeInfo, req *kvstore.PutRequest) (*kvstore.PutResponse, error) {
	cli, err := s.membership.GetClient(targetNode.NodeID)
	if err != nil {
		return &kvstore.PutResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to get client for node %s: %v", targetNode.NodeID, err),
		}, nil
	}

	// Forward the request
	if err := cli.Put(ctx, req.Key, req.Value); err != nil {
		return &kvstore.PutResponse{
			Success: false,
			Error:   fmt.Sprintf("forwarded request failed: %v", err),
		}, nil
	}

	return &kvstore.PutResponse{
		Success: true,
	}, nil
}

// forwardGet forwards a GET request to the target node
func (s *ShardedKVServer) forwardGet(ctx context.Context, targetNode *sharding.NodeInfo, req *kvstore.GetRequest) (*kvstore.GetResponse, error) {
	cli, err := s.membership.GetClient(targetNode.NodeID)
	if err != nil {
		return &kvstore.GetResponse{
			Found: false,
			Error: fmt.Sprintf("failed to get client for node %s: %v", targetNode.NodeID, err),
		}, nil
	}

	// Forward the request
	value, err := cli.Get(ctx, req.Key)
	if err != nil {
		if err.Error() == "get RPC failed: key not found" {
			return &kvstore.GetResponse{
				Found: false,
			}, nil
		}
		return &kvstore.GetResponse{
			Found: false,
			Error: fmt.Sprintf("forwarded request failed: %v", err),
		}, nil
	}

	return &kvstore.GetResponse{
		Found: true,
		Value: value,
	}, nil
}

// forwardDelete forwards a DELETE request to the target node
func (s *ShardedKVServer) forwardDelete(ctx context.Context, targetNode *sharding.NodeInfo, req *kvstore.DeleteRequest) (*kvstore.DeleteResponse, error) {
	cli, err := s.membership.GetClient(targetNode.NodeID)
	if err != nil {
		return &kvstore.DeleteResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to get client for node %s: %v", targetNode.NodeID, err),
		}, nil
	}

	// Forward the request
	if err := cli.Delete(ctx, req.Key); err != nil {
		return &kvstore.DeleteResponse{
			Success: false,
			Error:   fmt.Sprintf("forwarded request failed: %v", err),
		}, nil
	}

	return &kvstore.DeleteResponse{
		Success: true,
	}, nil
}

// performReadRepair performs read repair by checking replicas
func (s *ShardedKVServer) performReadRepair(key string, localValue []byte) {
	// Get replicas for this key
	replicas, err := s.membership.GetReplicas(key, s.replicationFactor)
	if err != nil {
		log.Printf("Read repair: failed to get replicas for key %s: %v", key, err)
		return
	}

	// Check each replica
	for _, replica := range replicas {
		if s.membership.IsLocalNode(replica.NodeID) {
			continue // Skip local node
		}

		cli, err := s.membership.GetClient(replica.NodeID)
		if err != nil {
			log.Printf("Read repair: failed to get client for replica %s: %v", replica.NodeID, err)
			continue
		}

		replicaValue, err := cli.Get(nil, key)
		if err != nil {
			// Replica doesn't have the key - repair it
			if err.Error() == "get RPC failed: key not found" {
				log.Printf("Read repair: replica %s missing key %s, repairing...", replica.NodeID, key)
				if err := cli.Put(nil, key, localValue); err != nil {
					log.Printf("Read repair: failed to repair replica %s: %v", replica.NodeID, err)
				} else {
					log.Printf("Read repair: successfully repaired replica %s", replica.NodeID)
				}
			}
			continue
		}

		// Compare values - if different, we have a conflict
		if !equalBytes(localValue, replicaValue) {
			log.Printf("Read repair: value mismatch for key %s between local and replica %s", key, replica.NodeID)
			// In a production system, you'd implement conflict resolution here
			// For now, we just log the conflict
		}
	}
}

// equalBytes compares two byte slices
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// StartShardedServer starts the gRPC server with sharding support
func StartShardedServer(api *storage.API, node *raft.Node, membership *sharding.MembershipService, address string) error {
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s := grpc.NewServer()
	kvServer := NewShardedKVServer(api, node, membership)
	kvstore.RegisterKeyValueStoreServer(s, kvServer)

	log.Printf("Sharded gRPC server listening on %s", address)
	log.Printf("RAFT node state: %s, leader: %s", node.State(), node.Leader())
	if err := s.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

// StartShardedServerWithContext starts the sharded gRPC server with graceful shutdown
func StartShardedServerWithContext(ctx context.Context, api *storage.API, node *raft.Node, membership *sharding.MembershipService, address string) error {
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s := grpc.NewServer()
	kvServer := NewShardedKVServer(api, node, membership)
	kvstore.RegisterKeyValueStoreServer(s, kvServer)

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		log.Println("Shutting down sharded gRPC server...")
		s.GracefulStop()
	}()

	log.Printf("Sharded gRPC server listening on %s", address)
	log.Printf("RAFT node state: %s, leader: %s", node.State(), node.Leader())
	if err := s.Serve(lis); err != nil && err != grpc.ErrServerStopped {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

