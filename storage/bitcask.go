package storage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Record represents a single key-value record in the log file
type Record struct {
	KeySize   uint32
	ValueSize uint32
	Key       []byte
	Value     []byte
}

// RecordLocation stores the location of a record in the log file
type RecordLocation struct {
	FileID uint32
	Offset int64
	Size   int64
}

// Bitcask is the main storage engine implementing Bitcask-style storage
type Bitcask struct {
	mu            sync.RWMutex
	keyDir        map[string]*RecordLocation // In-memory hash map: key -> location
	dataDir       string                      // Directory for data files
	currentFileID uint32                     // Current active data file ID
	currentFile   *os.File                   // Current active data file
	currentOffset int64                      // Current offset in the active file
	maxFileSize   int64                      // Maximum size before creating a new file
}

// NewBitcask creates a new Bitcask storage engine
func NewBitcask(dataDir string) (*Bitcask, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	bc := &Bitcask{
		keyDir:      make(map[string]*RecordLocation),
		dataDir:     dataDir,
		maxFileSize: 100 * 1024 * 1024, // 100MB default
	}

	// Recover existing data files
	if err := bc.recover(); err != nil {
		return nil, fmt.Errorf("failed to recover: %w", err)
	}

	// Open or create the current active file
	if err := bc.openCurrentFile(); err != nil {
		return nil, fmt.Errorf("failed to open current file: %w", err)
	}

	return bc, nil
}

// recover rebuilds the keyDir from existing data files
func (bc *Bitcask) recover() error {
	entries, err := os.ReadDir(bc.dataDir)
	if err != nil {
		return err
	}

	var maxFileID uint32 = 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		var fileID uint32
		if _, err := fmt.Sscanf(entry.Name(), "data-%d.log", &fileID); err != nil {
			continue // Skip files that don't match the pattern
		}

		if fileID > maxFileID {
			maxFileID = fileID
		}

		// Read all records from this file
		filePath := filepath.Join(bc.dataDir, entry.Name())
		if err := bc.readDataFile(filePath, fileID); err != nil {
			return fmt.Errorf("failed to read data file %s: %w", filePath, err)
		}
	}

	bc.currentFileID = maxFileID
	return nil
}

// readDataFile reads all records from a data file and rebuilds the keyDir
func (bc *Bitcask) readDataFile(filePath string, fileID uint32) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	var offset int64 = 0

	for {
		var keySize, valueSize uint32

		// Read key size
		if err := binary.Read(file, binary.LittleEndian, &keySize); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		// Read value size
		if err := binary.Read(file, binary.LittleEndian, &valueSize); err != nil {
			return err
		}

		// Read key
		key := make([]byte, keySize)
		if _, err := io.ReadFull(file, key); err != nil {
			return err
		}

		// Read value
		value := make([]byte, valueSize)
		if _, err := io.ReadFull(file, value); err != nil {
			return err
		}

		// Calculate record size
		recordSize := int64(8 + keySize + valueSize) // 8 bytes for keySize + valueSize

		// Update keyDir (latest record wins)
		bc.keyDir[string(key)] = &RecordLocation{
			FileID: fileID,
			Offset: offset,
			Size:   recordSize,
		}

		offset += recordSize
	}

	return nil
}

// openCurrentFile opens or creates the current active data file
func (bc *Bitcask) openCurrentFile() error {
	fileName := fmt.Sprintf("data-%d.log", bc.currentFileID)
	filePath := filepath.Join(bc.dataDir, fileName)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	// Get current file size
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}

	bc.currentFile = file
	bc.currentOffset = stat.Size()
	return nil
}

// rotateFile creates a new data file when the current one exceeds maxFileSize
func (bc *Bitcask) rotateFile() error {
	if bc.currentFile != nil {
		bc.currentFile.Close()
	}

	bc.currentFileID++
	return bc.openCurrentFile()
}

// Put stores a key-value pair
func (bc *Bitcask) Put(key, value []byte) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	// Check if we need to rotate the file
	if bc.currentOffset > bc.maxFileSize {
		if err := bc.rotateFile(); err != nil {
			return err
		}
	}

	// Write record to log file
	offset := bc.currentOffset

	// Write key size
	if err := binary.Write(bc.currentFile, binary.LittleEndian, uint32(len(key))); err != nil {
		return fmt.Errorf("failed to write key size: %w", err)
	}

	// Write value size
	if err := binary.Write(bc.currentFile, binary.LittleEndian, uint32(len(value))); err != nil {
		return fmt.Errorf("failed to write value size: %w", err)
	}

	// Write key
	if _, err := bc.currentFile.Write(key); err != nil {
		return fmt.Errorf("failed to write key: %w", err)
	}

	// Write value
	if _, err := bc.currentFile.Write(value); err != nil {
		return fmt.Errorf("failed to write value: %w", err)
	}

	// Ensure data is written to disk (durability)
	if err := bc.currentFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	// Calculate record size
	recordSize := int64(8 + len(key) + len(value))

	// Update keyDir
	bc.keyDir[string(key)] = &RecordLocation{
		FileID: bc.currentFileID,
		Offset: offset,
		Size:   recordSize,
	}

	bc.currentOffset += recordSize
	return nil
}

// Get retrieves a value by key
func (bc *Bitcask) Get(key []byte) ([]byte, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	location, exists := bc.keyDir[string(key)]
	if !exists {
		return nil, errors.New("key not found")
	}

	// Open the file containing the record
	fileName := fmt.Sprintf("data-%d.log", location.FileID)
	filePath := filepath.Join(bc.dataDir, fileName)

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open data file: %w", err)
	}
	defer file.Close()

	// Seek to the record location
	if _, err := file.Seek(location.Offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek: %w", err)
	}

	// Read key size and value size
	var keySize, valueSize uint32
	if err := binary.Read(file, binary.LittleEndian, &keySize); err != nil {
		return nil, fmt.Errorf("failed to read key size: %w", err)
	}
	if err := binary.Read(file, binary.LittleEndian, &valueSize); err != nil {
		return nil, fmt.Errorf("failed to read value size: %w", err)
	}

	// Skip the key (we already know it)
	if _, err := file.Seek(int64(keySize), io.SeekCurrent); err != nil {
		return nil, fmt.Errorf("failed to skip key: %w", err)
	}

	// Read the value
	value := make([]byte, valueSize)
	if _, err := io.ReadFull(file, value); err != nil {
		return nil, fmt.Errorf("failed to read value: %w", err)
	}

	return value, nil
}

// Delete marks a key as deleted by writing a tombstone record
func (bc *Bitcask) Delete(key []byte) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	// Check if key exists
	if _, exists := bc.keyDir[string(key)]; !exists {
		return errors.New("key not found")
	}

	// Write a tombstone record (value size = 0xFFFFFFFF indicates deletion)
	offset := bc.currentOffset

	// Write key size
	if err := binary.Write(bc.currentFile, binary.LittleEndian, uint32(len(key))); err != nil {
		return fmt.Errorf("failed to write key size: %w", err)
	}

	// Write value size as 0xFFFFFFFF (tombstone marker)
	tombstone := uint32(0xFFFFFFFF)
	if err := binary.Write(bc.currentFile, binary.LittleEndian, tombstone); err != nil {
		return fmt.Errorf("failed to write tombstone: %w", err)
	}

	// Write key
	if _, err := bc.currentFile.Write(key); err != nil {
		return fmt.Errorf("failed to write key: %w", err)
	}

	// Ensure data is written to disk
	if err := bc.currentFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	// Remove from keyDir
	delete(bc.keyDir, string(key))

	recordSize := int64(8 + len(key))
	bc.currentOffset += recordSize
	return nil
}

// Close closes the storage engine
func (bc *Bitcask) Close() error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.currentFile != nil {
		return bc.currentFile.Close()
	}
	return nil
}

