package client

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"titan-kv/proto/kvstore"
)

// Client is a gRPC client for the KV store
type Client struct {
	conn   *grpc.ClientConn
	client kvstore.KeyValueStoreClient
}

// NewClient creates a new gRPC client connected to the server
func NewClient(address string) (*Client, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return &Client{
		conn:   conn,
		client: kvstore.NewKeyValueStoreClient(conn),
	}, nil
}

// Close closes the client connection
func (c *Client) Close() error {
	return c.conn.Close()
}

// Put stores a key-value pair
func (c *Client) Put(ctx context.Context, key string, value []byte) error {
	if ctx == nil {
		ctx = context.Background()
	}

	req := &kvstore.PutRequest{
		Key:   key,
		Value: value,
	}

	resp, err := c.client.Put(ctx, req)
	if err != nil {
		return fmt.Errorf("put RPC failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("put failed: %s", resp.Error)
	}

	return nil
}

// Get retrieves a value by key
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	req := &kvstore.GetRequest{
		Key: key,
	}

	resp, err := c.client.Get(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get RPC failed: %w", err)
	}

	if !resp.Found {
		return nil, fmt.Errorf("key not found")
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("get error: %s", resp.Error)
	}

	return resp.Value, nil
}

// Delete removes a key-value pair
func (c *Client) Delete(ctx context.Context, key string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	req := &kvstore.DeleteRequest{
		Key: key,
	}

	resp, err := c.client.Delete(ctx, req)
	if err != nil {
		return fmt.Errorf("delete RPC failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("delete failed: %s", resp.Error)
	}

	return nil
}

// PutWithTimeout stores a key-value pair with a timeout
func (c *Client) PutWithTimeout(key string, value []byte, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.Put(ctx, key, value)
}

// GetWithTimeout retrieves a value by key with a timeout
func (c *Client) GetWithTimeout(key string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.Get(ctx, key)
}

// DeleteWithTimeout removes a key-value pair with a timeout
func (c *Client) DeleteWithTimeout(key string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.Delete(ctx, key)
}

