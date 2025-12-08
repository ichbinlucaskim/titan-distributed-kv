package storage

import (
	"errors"
	"fmt"
)

// API provides a high-level interface for the storage engine
type API struct {
	bc        *Bitcask
	compactor *Compactor
}

// NewAPI creates a new API instance
func NewAPI(bc *Bitcask, compactor *Compactor) *API {
	return &API{
		bc:        bc,
		compactor: compactor,
	}
}

// Put stores a key-value pair with durability guarantee
func (api *API) Put(key string, value []byte) error {
	if key == "" {
		return errors.New("key cannot be empty")
	}

	// Ensure durability: write to log file and sync
	if err := api.bc.Put([]byte(key), value); err != nil {
		return fmt.Errorf("put operation failed: %w", err)
	}

	return nil
}

// Get retrieves a value by key
func (api *API) Get(key string) ([]byte, error) {
	if key == "" {
		return nil, errors.New("key cannot be empty")
	}

	value, err := api.bc.Get([]byte(key))
	if err != nil {
		return nil, fmt.Errorf("get operation failed: %w", err)
	}

	return value, nil
}

// Delete removes a key-value pair with durability guarantee
func (api *API) Delete(key string) error {
	if key == "" {
		return errors.New("key cannot be empty")
	}

	// Ensure durability: write tombstone to log file and sync
	if err := api.bc.Delete([]byte(key)); err != nil {
		return fmt.Errorf("delete operation failed: %w", err)
	}

	return nil
}

// Compact triggers garbage collection
func (api *API) Compact() error {
	return api.compactor.Compact()
}

// Close closes the storage engine
func (api *API) Close() error {
	return api.bc.Close()
}

