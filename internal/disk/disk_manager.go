package disk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	PageSize    = 8192             // 8 kB
	SegmentSize = 64 * 1024 * 1024 // 64 MiB
)

type (
	PageID  uint32
	TableID uint32
)

var (
	ErrInvalidRoot    = errors.New("invalid database root")
	ErrInvalidTableId = errors.New("id table not found")
	ErrInvalidPageId  = errors.New("id page not found")
	ErrInvalidData    = errors.New("the page size and the size of the data read do not match")
)

type DiskManager struct {
	root       string
	dataDir    string
	catalogDir string
	segments   map[string]*os.File
	tables     map[TableID]string
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
		segments:   make(map[string]*os.File),
		tables:     make(map[TableID]string),
		mu:         sync.RWMutex{},
	}, nil
}

func (dm *DiskManager) ReadPage(tableid TableID, pid PageID) ([]byte, error) {
	dm.mu.RLock()
	tabDir, ok := dm.tables[tableid]
	if !ok {
		dm.mu.RUnlock()
		return nil, ErrInvalidTableId
	}

	segmentId, offset := pageLocation(pid)
	segmentDir := filepath.Join(tabDir, "segment"+strconv.Itoa(int(segmentId)))
	segmentFile, ok := dm.segments[segmentDir]
	if !ok {
		dm.mu.RUnlock()
		return nil, ErrInvalidPageId
	}
	dm.mu.RUnlock()

	data := make([]byte, PageSize)
	dataSize, err := segmentFile.ReadAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if dataSize != PageSize {
		return nil, ErrInvalidData
	}

	return data, nil
}

func pageLocation(pid PageID) (segmentID uint32, offset int64) {
	pagesPerSegment := SegmentSize / PageSize

	segmentID = uint32(pid / PageID(pagesPerSegment))
	pageInSegment := pid % PageID(pagesPerSegment)

	offset = int64(pageInSegment * PageSize)

	return
}
