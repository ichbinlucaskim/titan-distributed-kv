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
	"titan-kv/storage"
)

// KVServer implements the KeyValueStore gRPC service
type KVServer struct {
	kvstore.UnimplementedKeyValueStoreServer
	api *storage.API
}

// NewKVServer creates a new gRPC server instance
func NewKVServer(api *storage.API) *KVServer {
	return &KVServer{
		api: api,
	}
}

// Put implements the Put RPC method
func (s *KVServer) Put(ctx context.Context, req *kvstore.PutRequest) (*kvstore.PutResponse, error) {
	if req.Key == "" {
		return &kvstore.PutResponse{
			Success: false,
			Error:   "key cannot be empty",
		}, nil
	}

	if err := s.api.Put(req.Key, req.Value); err != nil {
		return &kvstore.PutResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &kvstore.PutResponse{
		Success: true,
	}, nil
}

// Get implements the Get RPC method
func (s *KVServer) Get(ctx context.Context, req *kvstore.GetRequest) (*kvstore.GetResponse, error) {
	if req.Key == "" {
		return &kvstore.GetResponse{
			Found: false,
			Error: "key cannot be empty",
		}, nil
	}

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

// Delete implements the Delete RPC method
func (s *KVServer) Delete(ctx context.Context, req *kvstore.DeleteRequest) (*kvstore.DeleteResponse, error) {
	if req.Key == "" {
		return &kvstore.DeleteResponse{
			Success: false,
			Error:   "key cannot be empty",
		}, nil
	}

	if err := s.api.Delete(req.Key); err != nil {
		// Check if it's a "key not found" error
		if err.Error() == "delete operation failed: key not found" {
			return &kvstore.DeleteResponse{
				Success: false,
				Error:   "key not found",
			}, nil
		}
		return &kvstore.DeleteResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &kvstore.DeleteResponse{
		Success: true,
	}, nil
}

// StartServer starts the gRPC server on the specified address
func StartServer(api *storage.API, address string) error {
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s := grpc.NewServer()
	kvServer := NewKVServer(api)
	kvstore.RegisterKeyValueStoreServer(s, kvServer)

	log.Printf("gRPC server listening on %s", address)
	if err := s.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

// StartServerWithContext starts the gRPC server with graceful shutdown support
func StartServerWithContext(ctx context.Context, api *storage.API, address string) error {
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s := grpc.NewServer()
	kvServer := NewKVServer(api)
	kvstore.RegisterKeyValueStoreServer(s, kvServer)

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		log.Println("Shutting down gRPC server...")
		s.GracefulStop()
	}()

	log.Printf("gRPC server listening on %s", address)
	if err := s.Serve(lis); err != nil && err != grpc.ErrServerStopped {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

