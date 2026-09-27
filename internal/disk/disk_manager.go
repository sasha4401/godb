package disk

import (
	"errors"
	"fmt"
	"io"
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
	ErrInvalidTableID = errors.New("id table not found")
	ErrInvalidPageID  = errors.New("id page not found")
	ErrInvalidData    = errors.New("the page size and the size of the data read do not match")
	ErrOpenFile       = errors.New("error open file")
)

type DiskManager struct {
	root       string
	dataDir    string
	catalogDir string
	segment    map[string]*os.File
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
		segment:    make(map[string]*os.File),
		tables:     make(map[TableID]string),
		mu:         sync.RWMutex{},
	}, nil
}

func (dm *DiskManager) getFile(path string) (*os.File, error) {
	dm.mu.RLock()
	file, ok := dm.segment[path]
	dm.mu.RUnlock()
	if ok {
		return file, nil
	}

	dm.mu.Lock()
	defer dm.mu.Unlock()

	if file, ok = dm.segment[path]; ok {
		return file, nil
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o640)
	if err != nil {
		return nil, err
	}

	dm.segment[path] = file
	return file, nil
}

func (dm *DiskManager) ReadPage(tableid TableID, pid PageID, destFrame []byte) error {
	dm.mu.RLock()
	tabDir, ok := dm.tables[tableid]
	dm.mu.RUnlock()
	if !ok {
		return ErrInvalidTableID
	}

	segmentID, offset := pageLocation(pid)
	segmentDir := filepath.Join(tabDir, "segment"+strconv.Itoa(int(segmentID)))
	file, err := dm.getFile(segmentDir)
	if err != nil {
		return ErrOpenFile
	}

	dataSize, err := file.ReadAt(destFrame, offset)
	if errors.Is(err, io.EOF) && dataSize < len(destFrame) {
		for i := dataSize; i < len(destFrame); i++ {
			destFrame[i] = 0
		}
		return nil
	}

	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to read file: %w", err)
	}

	if dataSize != PageSize {
		return ErrInvalidData
	}

	return nil
}

func (dm *DiskManager) WritePage(tableid TableID, pid PageID, srcFrame []byte) error {
	if len(srcFrame) != PageSize {
		return ErrInvalidData
	}

	dm.mu.RLock()
	tabDir, ok := dm.tables[tableid]
	dm.mu.RUnlock()
	if !ok {
		return ErrInvalidTableID
	}

	segmentID, offset := pageLocation(pid)
	segmentDir := filepath.Join(tabDir, "segment"+strconv.Itoa(int(segmentID)))
	file, err := dm.getFile(segmentDir)
	if err != nil {
		return ErrOpenFile
	}

	n, err := file.WriteAt(srcFrame, offset)
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	if n != PageSize {
		return ErrInvalidData
	}

	return nil

}

func pageLocation(pid PageID) (segmentID uint32, offset int64) {
	pagesPerSegment := SegmentSize / PageSize

	segmentID = uint32(pid / PageID(pagesPerSegment))
	pageInSegment := pid % PageID(pagesPerSegment)

	offset = int64(pageInSegment * PageSize)

	return
}
