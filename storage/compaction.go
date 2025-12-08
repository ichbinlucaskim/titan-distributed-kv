package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Compactor handles garbage collection and log compaction
type Compactor struct {
	bc     *Bitcask
	mu     sync.Mutex
	active bool
}

// NewCompactor creates a new compactor for the given Bitcask instance
func NewCompactor(bc *Bitcask) *Compactor {
	return &Compactor{
		bc: bc,
	}
}

// Compact performs garbage collection by merging data files
// It creates a new merged file containing only the latest version of each key
func (c *Compactor) Compact() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.active {
		return fmt.Errorf("compaction already in progress")
	}

	c.active = true
	defer func() {
		c.active = false
	}()

	c.bc.mu.Lock()
	defer c.bc.mu.Unlock()

	// Get all data files
	entries, err := os.ReadDir(c.bc.dataDir)
	if err != nil {
		return fmt.Errorf("failed to read data directory: %w", err)
	}

	// Find the highest file ID
	var maxFileID uint32 = 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		var fileID uint32
		if _, err := fmt.Sscanf(entry.Name(), "data-%d.log", &fileID); err != nil {
			continue
		}

		if fileID > maxFileID {
			maxFileID = fileID
		}
	}

	// Don't compact if there's only one file or no files
	if maxFileID == 0 {
		return nil
	}

	// Create a new merged file
	mergedFileID := maxFileID + 1
	mergedFileName := fmt.Sprintf("data-%d.log", mergedFileID)
	mergedFilePath := filepath.Join(c.bc.dataDir, mergedFileName)

	mergedFile, err := os.Create(mergedFilePath)
	if err != nil {
		return fmt.Errorf("failed to create merged file: %w", err)
	}
	defer mergedFile.Close()

	// Write all active keys to the merged file
	var newOffset int64 = 0
	newKeyDir := make(map[string]*RecordLocation)

	for key, location := range c.bc.keyDir {
		// Read the record from the old file
		oldFileName := fmt.Sprintf("data-%d.log", location.FileID)
		oldFilePath := filepath.Join(c.bc.dataDir, oldFileName)

		oldFile, err := os.Open(oldFilePath)
		if err != nil {
			// If file doesn't exist, skip this key
			continue
		}

		// Seek to the record location
		if _, err := oldFile.Seek(location.Offset, io.SeekStart); err != nil {
			oldFile.Close()
			continue
		}

		// Read the entire record
		record := make([]byte, location.Size)
		if _, err := oldFile.Read(record); err != nil {
			oldFile.Close()
			continue
		}
		oldFile.Close()

		// Write the record to the merged file
		if _, err := mergedFile.Write(record); err != nil {
			return fmt.Errorf("failed to write to merged file: %w", err)
		}

		// Update the keyDir entry
		newKeyDir[key] = &RecordLocation{
			FileID: mergedFileID,
			Offset: newOffset,
			Size:   location.Size,
		}

		newOffset += location.Size
	}

	// Sync the merged file
	if err := mergedFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync merged file: %w", err)
	}

	// Replace old files with the merged file
	// First, close the current file if it's not the merged file
	if c.bc.currentFile != nil && c.bc.currentFileID != mergedFileID {
		c.bc.currentFile.Close()
	}

	// Delete old data files (except the merged one)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		var fileID uint32
		if _, err := fmt.Sscanf(entry.Name(), "data-%d.log", &fileID); err != nil {
			continue
		}

		// Don't delete the merged file or files newer than it
		if fileID >= mergedFileID {
			continue
		}

		filePath := filepath.Join(c.bc.dataDir, entry.Name())
		if err := os.Remove(filePath); err != nil {
			// Log error but continue
			fmt.Printf("Warning: failed to remove old file %s: %v\n", filePath, err)
		}
	}

	// Update Bitcask with new keyDir and file info
	c.bc.keyDir = newKeyDir
	c.bc.currentFileID = mergedFileID
	c.bc.currentOffset = newOffset

	// Reopen the merged file as the current file
	if err := c.bc.openCurrentFile(); err != nil {
		return fmt.Errorf("failed to reopen merged file: %w", err)
	}

	return nil
}

// IsActive returns whether compaction is currently in progress
func (c *Compactor) IsActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

