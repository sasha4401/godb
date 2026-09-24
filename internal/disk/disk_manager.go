package disk

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
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
		tables:     make(map[TableID]string),
		mu:         sync.RWMutex{},
	}, nil
}

func (dm *DiskManager) ReadPage(tableid TableID, pid PageID) ([]byte, error) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	tabDir, ok := dm.tables[tableid]
	if !ok {
		return nil, ErrInvalidTableId
	}

	segmentId, offset := pageLocation(pid)
	segmentDir := filepath.Join(tabDir, "segment"+strconv.Itoa(int(segmentId)))
	file, err := os.OpenFile(segmentDir, os.O_RDWR|os.O_CREATE|syscall.O_DIRECT, 0o640)
	if err != nil {
		return nil, fmt.Errorf("failed to open file with O_DIRECT: %w", err)
	}
	defer file.Close()

	data, err := makeAlignedBuffer(PageSize)
	if err != nil {
		return nil, err
	}
	defer syscall.Munmap(data)

	dataSize, err := file.ReadAt(data, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if dataSize != PageSize {
		return nil, ErrInvalidData
	}

	result := make([]byte, PageSize)
	copy(result, data)

	return result, nil
}

func pageLocation(pid PageID) (segmentID uint32, offset int64) {
	pagesPerSegment := SegmentSize / PageSize

	segmentID = uint32(pid / PageID(pagesPerSegment))
	pageInSegment := pid % PageID(pagesPerSegment)

	offset = int64(pageInSegment * PageSize)

	return
}

func makeAlignedBuffer(size int) ([]byte, error) {
	block, err := syscall.Mmap(-1, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate aligned memory: %w", err)
	}

	return block, nil
}
