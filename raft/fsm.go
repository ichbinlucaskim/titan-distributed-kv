package raft

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/hashicorp/raft"
	"titan-kv/storage"
)

// LogEntry represents a single log entry in the RAFT log
type LogEntry struct {
	Operation string `json:"operation"` // "PUT" or "DELETE"
	Key       string `json:"key"`
	Value     []byte `json:"value"` // Empty for DELETE operations
}

// FSM implements the raft.FSM interface for applying log entries to the storage engine
type FSM struct {
	mu    sync.RWMutex
	api   *storage.API
	index uint64 // Last applied index
}

// NewFSM creates a new FSM instance
func NewFSM(api *storage.API) *FSM {
	return &FSM{
		api:   api,
		index: 0,
	}
}

// Apply applies a log entry to the FSM
func (f *FSM) Apply(log *raft.Log) interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()

	var entry LogEntry
	if err := json.Unmarshal(log.Data, &entry); err != nil {
		return fmt.Errorf("failed to unmarshal log entry: %w", err)
	}

	var err error
	switch entry.Operation {
	case "PUT":
		err = f.api.Put(entry.Key, entry.Value)
		if err != nil {
			return fmt.Errorf("failed to apply PUT: %w", err)
		}
	case "DELETE":
		err = f.api.Delete(entry.Key)
		if err != nil {
			return fmt.Errorf("failed to apply DELETE: %w", err)
		}
	default:
		return fmt.Errorf("unknown operation: %s", entry.Operation)
	}

	f.index = log.Index
	return nil
}

// Snapshot returns a snapshot of the current state
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// For simplicity, we return a snapshot that can be used to restore state
	// In a production system, this would serialize the entire keyDir
	return &Snapshot{
		index: f.index,
	}, nil
}

// Restore restores the FSM from a snapshot
func (f *FSM) Restore(rc io.ReadCloser) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// For simplicity, we just read and discard the snapshot
	// In a production system, this would restore the keyDir from the snapshot
	defer rc.Close()

	// The storage engine will rebuild its state from the log files on startup
	// So we don't need to restore the keyDir here
	return nil
}

// GetLastIndex returns the last applied index
func (f *FSM) GetLastIndex() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.index
}

// Snapshot implements the raft.FSMSnapshot interface
type Snapshot struct {
	index uint64
}

// Persist saves the snapshot to the given sink
func (s *Snapshot) Persist(sink raft.SnapshotSink) error {
	// Serialize the snapshot
	data, err := json.Marshal(map[string]interface{}{
		"index": s.index,
	})
	if err != nil {
		sink.Cancel()
		return err
	}

	if _, err := sink.Write(data); err != nil {
		sink.Cancel()
		return err
	}

	return sink.Close()
}

// Release releases resources after the snapshot is no longer needed
func (s *Snapshot) Release() {
	// Nothing to release
}

