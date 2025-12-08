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
	"titan-kv/storage"
)

// RaftKVServer implements the KeyValueStore gRPC service with RAFT consensus
type RaftKVServer struct {
	kvstore.UnimplementedKeyValueStoreServer
	api  *storage.API
	node *raft.Node
}

// NewRaftKVServer creates a new gRPC server instance with RAFT support
func NewRaftKVServer(api *storage.API, node *raft.Node) *RaftKVServer {
	return &RaftKVServer{
		api:  api,
		node: node,
	}
}

// Put implements the Put RPC method with RAFT consensus
func (s *RaftKVServer) Put(ctx context.Context, req *kvstore.PutRequest) (*kvstore.PutResponse, error) {
	if req.Key == "" {
		return &kvstore.PutResponse{
			Success: false,
			Error:   "key cannot be empty",
		}, nil
	}

	// Check if this node is the leader
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

// Get implements the Get RPC method (reads are local, no RAFT needed)
func (s *RaftKVServer) Get(ctx context.Context, req *kvstore.GetRequest) (*kvstore.GetResponse, error) {
	if req.Key == "" {
		return &kvstore.GetResponse{
			Found: false,
			Error: "key cannot be empty",
		}, nil
	}

	// Reads can be served directly from local storage
	// In a production system, you might want to ensure read consistency
	value, err := s.api.Get(req.Key)
	if err != nil {
		// Check if it's a "key not found" error
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

	return &kvstore.GetResponse{
		Found: true,
		Value: value,
	}, nil
}

// Delete implements the Delete RPC method with RAFT consensus
func (s *RaftKVServer) Delete(ctx context.Context, req *kvstore.DeleteRequest) (*kvstore.DeleteResponse, error) {
	if req.Key == "" {
		return &kvstore.DeleteResponse{
			Success: false,
			Error:   "key cannot be empty",
		}, nil
	}

	// Check if this node is the leader
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

// StartRaftServer starts the gRPC server with RAFT support
func StartRaftServer(api *storage.API, node *raft.Node, address string) error {
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s := grpc.NewServer()
	kvServer := NewRaftKVServer(api, node)
	kvstore.RegisterKeyValueStoreServer(s, kvServer)

	log.Printf("gRPC server with RAFT listening on %s", address)
	log.Printf("RAFT node state: %s, leader: %s", node.State(), node.Leader())
	if err := s.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

// StartRaftServerWithContext starts the gRPC server with RAFT and graceful shutdown support
func StartRaftServerWithContext(ctx context.Context, api *storage.API, node *raft.Node, address string) error {
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s := grpc.NewServer()
	kvServer := NewRaftKVServer(api, node)
	kvstore.RegisterKeyValueStoreServer(s, kvServer)

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		log.Println("Shutting down gRPC server...")
		s.GracefulStop()
	}()

	log.Printf("gRPC server with RAFT listening on %s", address)
	log.Printf("RAFT node state: %s, leader: %s", node.State(), node.Leader())
	if err := s.Serve(lis); err != nil && err != grpc.ErrServerStopped {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

