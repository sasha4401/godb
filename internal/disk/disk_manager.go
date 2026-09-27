package disk

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
)

const (
	PageSize    = 8192             // 8 kB
	SegmentSize = 64 * 1024 * 1024 // 64 MiB
)

var (
	ErrInvalidRoot      = errors.New("invalid database root")
	ErrInvalidTableID   = errors.New("id table not found")
	ErrInvalidPageID    = errors.New("id page not found")
	ErrInvalidData      = errors.New("the page size and the size of the data read do not match")
	ErrOpenFile         = errors.New("error open file")
	ErrInvalidDirectory = errors.New("invalid directory")
)

type (
	PageID  uint64
	TableID uint64
)

type fileDescriptor struct {
	path string
	file *os.File
	ref  atomic.Bool
}

type DiskManager struct {
	root         string
	dataDir      string
	catalogDir   string
	maxOpenFiles uint16
	segment      map[string]*fileDescriptor
	clockRing    []*fileDescriptor
	hand         int
	tables       map[TableID]string
	mu           sync.RWMutex
}

func Initialize(root string, maxOpenFiles uint16) (*DiskManager, error) {
	if root == "" {
		return nil, ErrInvalidRoot
	}

	if maxOpenFiles == 0 {
		maxOpenFiles = 500
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

	tables := make(map[TableID]string)
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read data directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			id, err := strconv.ParseUint(entry.Name(), 10, 64)
			if err == nil {
				tables[TableID(id)] = filepath.Join(dataDir, entry.Name())
			}
		}
	}

	return &DiskManager{
		root:         root,
		dataDir:      dataDir,
		catalogDir:   catalogDir,
		maxOpenFiles: maxOpenFiles,
		segment:      make(map[string]*fileDescriptor),
		clockRing:    make([]*fileDescriptor, 0, maxOpenFiles),
		tables:       tables,
		mu:           sync.RWMutex{},
	}, nil
}

func (dm *DiskManager) getFile(path string) (*os.File, error) {
	dm.mu.RLock()
	if entry, ok := dm.segment[path]; ok {
		entry.ref.Store(true)
		dm.mu.RUnlock()
		return entry.file, nil
	}
	dm.mu.RUnlock()

	dm.mu.Lock()
	defer dm.mu.Unlock()

	if entry, ok := dm.segment[path]; ok {
		entry.ref.Store(true)
		return entry.file, nil
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o640)
	if err != nil {
		return nil, err
	}

	newEntry := &fileDescriptor{
		path: path,
		file: file,
	}
	newEntry.ref.Store(true)

	if len(dm.clockRing) < int(dm.maxOpenFiles) {
		dm.clockRing = append(dm.clockRing, newEntry)
		dm.segment[path] = newEntry
		return file, nil
	}

	for {
		victim := dm.clockRing[dm.hand]
		if victim.ref.Swap(false) {
			dm.hand = (dm.hand + 1) % int(dm.maxOpenFiles)
			continue
		}

		_ = victim.file.Close()
		delete(dm.segment, victim.path)
		dm.clockRing[dm.hand] = newEntry
		dm.segment[path] = newEntry
		dm.hand = (dm.hand + 1) % int(dm.maxOpenFiles)

		return file, nil
	}
}

func (dm *DiskManager) ReadPage(tableid TableID, pid PageID, destFrame []byte) error {
	dm.mu.RLock()
	tabDir, ok := dm.tables[tableid]
	dm.mu.RUnlock()
	if !ok {
		return ErrInvalidTableID
	}

	segmentID, offset := pageLocation(pid)
	segmentDir := filepath.Join(tabDir, "segment"+strconv.FormatUint(segmentID, 10))
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
	segmentDir := filepath.Join(tabDir, "segment"+strconv.FormatUint(segmentID, 10))
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

func (dm *DiskManager) CreateTableDir(tableID TableID) error {
	tableDir := filepath.Join(dm.dataDir, strconv.FormatUint(uint64(tableID), 10))
	err := os.MkdirAll(tableDir, 0o750)
	if err != nil {
		return ErrInvalidDirectory
	}

	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.tables[tableID] = tableDir

	return nil
}

func (dm *DiskManager) Close() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	var errs []error
	for path, fileDesc := range dm.segment {
		if err := fileDesc.file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close %s: %w", path, err))
		}
	}

	dm.segment = make(map[string]*fileDescriptor)
	dm.clockRing = nil
	dm.hand = 0

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func pageLocation(pid PageID) (segmentID uint64, offset int64) {
	pagesPerSegment := SegmentSize / PageSize

	segmentID = uint64(pid / PageID(pagesPerSegment))
	pageInSegment := pid % PageID(pagesPerSegment)

	offset = int64(pageInSegment * PageSize)

	return
}
