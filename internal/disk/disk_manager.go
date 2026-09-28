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

// default database page size and on-disk page segment size
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
	path  string
	file  *os.File
	ref   atomic.Bool //state in the clock algorithm
	inUse atomic.Int32
}

// structure responsible for writing and reading pages from the disk
type DiskManager struct {
	//database root directory
	root string
	//table data directory
	dataDir string
	//catalog of pages directory
	catalogDir string
	//maximum number of simultaneously open file descriptors for table segments
	maxOpenFiles uint16
	//file descriptor catalog for segment files
	segment map[string]*fileDescriptor
	//Data structures for the operation of the Clock algorithm,
	//comprising a set of candidate descriptors for closure and the current pointer position.
	clockRing []*fileDescriptor
	hand      int
	//catalog of tables
	tables map[TableID]string
	mu     sync.RWMutex
}

// Disk manager initialization method.
// Its tasks are:
// 1) to create directories if they do not exist;
// 2) Discovers and maps existing tables by scanning the data directory;
// 3) to set the initial state of the file descriptor closing algorithm.
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

// method for obtaining a file descriptor
func (dm *DiskManager) getFile(path string) (*fileDescriptor, error) {
	dm.mu.RLock()
	if entry, ok := dm.segment[path]; ok {
		entry.ref.Store(true)
		entry.inUse.Add(1)
		dm.mu.RUnlock()
		return entry, nil
	}
	dm.mu.RUnlock()

	dm.mu.Lock()
	defer dm.mu.Unlock()

	if entry, ok := dm.segment[path]; ok {
		entry.ref.Store(true)
		entry.inUse.Add(1)
		return entry, nil
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o640)
	if err != nil {
		return nil, err
	}

	newEntry := &fileDescriptor{
		path: path,
		file: file,
	}
	newEntry.inUse.Add(1)
	newEntry.ref.Store(true)

	if len(dm.clockRing) < int(dm.maxOpenFiles) {
		dm.clockRing = append(dm.clockRing, newEntry)
		dm.segment[path] = newEntry
		return newEntry, nil
	}

	//the process of searching for an unnecessary descriptor using the clock algorithm
	for {
		victim := dm.clockRing[dm.hand]
		if victim.ref.Swap(false) || victim.inUse.Load() != 0 {
			dm.hand = (dm.hand + 1) % int(dm.maxOpenFiles)
			continue
		}

		_ = victim.file.Close()
		delete(dm.segment, victim.path)
		dm.clockRing[dm.hand] = newEntry
		dm.segment[path] = newEntry
		dm.hand = (dm.hand + 1) % int(dm.maxOpenFiles)

		return newEntry, nil
	}
}

// The ReadPage method reads page data from the segment file into the destination buffer.
func (dm *DiskManager) ReadPage(tableid TableID, pid PageID, destFrame []byte) error {
	if len(destFrame) != PageSize {
		return ErrInvalidData
	}

	dm.mu.RLock()
	tabDir, ok := dm.tables[tableid]
	dm.mu.RUnlock()
	if !ok {
		return ErrInvalidTableID
	}

	segmentID, offset := pageLocation(pid)
	segmentPath := filepath.Join(tabDir, "segment"+strconv.FormatUint(segmentID, 10))
	fileDesc, err := dm.getFile(segmentPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOpenFile, err)
	}
	file := fileDesc.file
	dataSize, err := file.ReadAt(destFrame, offset)
	defer fileDesc.inUse.Add(-1)
	if errors.Is(err, io.EOF) && dataSize < len(destFrame) {
		for i := dataSize; i < len(destFrame); i++ {
			destFrame[i] = 0
		}
		return nil
	}

	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to read file: %w", err)
	}

	return nil
}

// The write-page method reads data from the buffer and writes it to a segment file on disk.
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
	segmentPath := filepath.Join(tabDir, "segment"+strconv.FormatUint(segmentID, 10))
	fileDesc, err := dm.getFile(segmentPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOpenFile, err)
	}

	file := fileDesc.file
	n, err := file.WriteAt(srcFrame, offset)
	defer fileDesc.inUse.Add(-1)
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	if n != PageSize {
		return ErrInvalidData
	}

	return nil

}

// creates a directory for the table if it did not previously exist
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

// a method called after the database has finished its work to close all file descriptors
func (dm *DiskManager) Close() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	var errs []error
	for path, fileDesc := range dm.segment {
		if fileDesc.inUse.Load() != 0 {
			errs = append(errs, errors.New("fail in use"))
			continue
		}

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

// a function for calculating the segment assignment and file offset to locate the position of the target page
func pageLocation(pid PageID) (segmentID uint64, offset int64) {
	pagesPerSegment := SegmentSize / PageSize

	segmentID = uint64(pid / PageID(pagesPerSegment))
	pageInSegment := pid % PageID(pagesPerSegment)

	offset = int64(pageInSegment * PageSize)

	return
}

/*
TODO: DiskManager — довести управление file descriptors до production-ready состояния.

Что проверить и доделать:

1. [ ] Исправить race в getFile() на cache hit.
    Сейчас в RLock-ветке:
        dm.mu.RUnlock()
        entry.inUse.Add(1)

    Между этими операциями другой goroutine может удалить и закрыть
    descriptor. Поэтому inUse нужно увеличивать ДО RUnlock().

2. [ ] Добавить sync.Cond для ситуации, когда все file descriptors заняты.
    Если maxOpenFiles достигнут и все descriptors имеют inUse > 0,
    Clock сейчас может бесконечно ходить по ring.

    Ожидаемое поведение:
        - нет свободного descriptor -> cond.Wait()
        - release descriptor -> cond.Signal()

3. [ ] Пересмотреть модель синхронизации.
    Сейчас одновременно используются:
        - sync.RWMutex
        - atomic.Bool
        - atomic.Int32
        - sync.Cond

    Подумать, не упростить ли это до:
        sync.Mutex + обычные bool/int + sync.Cond

    В таком варианте весь cache state защищается одним mutex.

4. [ ] Вынести освобождение descriptor в отдельный метод:
        releaseFile(fd *fileDescriptor)

    Он должен:
        - уменьшить inUse
        - разбудить ожидающий getFile()

5. [ ] Проверить Clock eviction.
    Evict можно только если:
        ref == false
        inUse == 0

    Отдельно проверить поведение second chance:
        ref == true -> сбросить ref и дать второй шанс
        ref == false && inUse == 0 -> eviction

6. [ ] Проверить Close().
    Сейчас если часть descriptors имеет inUse > 0,
    Close() возвращает ошибку, но всё равно очищает:
        segment
        clockRing

    Нужно выбрать нормальную семантику:
        - либо Close() ждёт, пока все операции закончатся;
        - либо возвращает ошибку и НЕ разрушает состояние DiskManager.

7. [ ] Добавить closed state.
    После Close() новые ReadPage/WritePage не должны молча
    заново открывать файлы.

8. [ ] Проверить валидацию ReadPage().
    До ReadAt проверить:
        len(destFrame) == PageSize

9. [ ] Проверить WritePage().
    До WriteAt проверить:
        len(srcFrame) == PageSize

10. [ ] Разделить ошибки.
     Сейчас ErrInvalidData используется для разных ситуаций.
     Подумать над отдельными ошибками для:
        - invalid page size
        - short read
        - short write

11. [ ] Сохранить underlying errors.
     При os.Open / os.ReadAt / os.WriteAt / Close использовать %w,
     чтобы не терять исходную ошибку ОС.

12. [ ] Написать тесты на eviction.
     Например maxOpenFiles = 2:
        - открыть A
        - открыть B
        - обратиться к A
        - открыть C
        - проверить, что Clock выбрал правильный victim.

13. [ ] Написать тест на inUse.
     Пока descriptor используется ReadAt/WriteAt,
     Clock не должен иметь возможность его закрыть.

14. [ ] Написать конкурентный тест.
     Несколько goroutine одновременно читают/пишут страницы
     при маленьком maxOpenFiles.

15. [ ] Запустить:
        go test -race ./...

     Особенно проверить:
        - getFile
        - eviction
        - Close
        - concurrent ReadPage/WritePage

16. [ ] После этого отдельно продумать durability.
     WriteAt != fsync.
     Решить позже, кто отвечает за Sync:
        DiskManager / WAL / BufferPoolManager.

Основные инварианты DiskManager:

    ref   -> descriptor недавно использовался Clock
    inUse -> descriptor сейчас используется goroutine

    eviction разрешён только когда:
        ref == false && inUse == 0

    getFile() должен возвращать descriptor уже с:
        inUse == 1

    ReadPage()/WritePage() должны гарантированно сделать:
        releaseFile() после завершения работы с descriptor.
*/
