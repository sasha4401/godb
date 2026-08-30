package disk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	PageSize          = 8192             // 8 kB
	SegmentSize       = 64 * 1024 * 1024 // 64 MiB
	SegmentHeaderSize = PageSize
)

type PageID uint32
type SegmentID uint32

var ErrInvalidRoot = errors.New("invalid database root")

type DiskManager struct {
	root       string
	dataDir    string
	catalogDir string
	segments   map[SegmentID]*os.File
	mu         sync.RWMutex
}

func Initialize(root string) (*DiskManager, error) {
	if root == "" {
		return nil, ErrInvalidRoot
	}

	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create directory error: %w", err)
	}

	dataDir := filepath.Join(root, "data")
	catalogDir := filepath.Join(root, "catalog")
	directory := []string{
		dataDir,
		catalogDir,
	}

	for _, dir := range directory {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create directory error: %w", err)
		}
	}

	return &DiskManager{
		root:       root,
		dataDir:    dataDir,
		catalogDir: catalogDir,
		segments:   make(map[SegmentID]*os.File),
		mu:         sync.RWMutex{},
	}, nil
}
