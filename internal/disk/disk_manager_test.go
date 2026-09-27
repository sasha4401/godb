package disk

import (
	"bytes"
	"testing"
)

func TestDiskManager_WriteAndReadPage(t *testing.T) {
	root := t.TempDir()
	dm, err := Initialize(root, 10)
	if err != nil {
		t.Fatalf("Failed to initialize DiskManager: %v", err)
	}
	defer dm.Close()

	tableID := TableID(1)
	if err := dm.CreateTableDir(tableID); err != nil {
		t.Fatalf("Failed to create table dir: %v", err)
	}

	pageID := PageID(0)
	writeData := make([]byte, PageSize)
	copy(writeData, []byte("hello database world"))

	if err := dm.WritePage(tableID, pageID, writeData); err != nil {
		t.Fatalf("WritePage failed: %v", err)
	}

	readData := make([]byte, PageSize)
	if err := dm.ReadPage(tableID, pageID, readData); err != nil {
		t.Fatalf("ReadPage failed: %v", err)
	}

	if !bytes.Equal(writeData, readData) {
		t.Errorf("Read data does not match written data")
	}
}

func TestDiskManager_ClockEviction(t *testing.T) {
	root := t.TempDir()
	dm, err := Initialize(root, 2)
	if err != nil {
		t.Fatalf("Failed to initialize DiskManager: %v", err)
	}
	defer dm.Close()

	tableID := TableID(1)
	_ = dm.CreateTableDir(tableID)

	pages := []PageID{0, 8192, 16384}

	for i, pid := range pages {
		data := make([]byte, PageSize)
		data[0] = byte(i + 1)

		if err := dm.WritePage(tableID, pid, data); err != nil {
			t.Fatalf("Failed to write page %d: %v", pid, err)
		}
	}

	dm.mu.RLock()
	openFilesCount := len(dm.clockRing)
	dm.mu.RUnlock()

	if openFilesCount > 2 {
		t.Errorf("Clock eviction failed: expected at most 2 open files, got %d", openFilesCount)
	}

	readData := make([]byte, PageSize)
	if err := dm.ReadPage(tableID, pages[0], readData); err != nil {
		t.Fatalf("Failed to read evicted page: %v", err)
	}

	if readData[0] != 1 {
		t.Errorf("Data mismatch after re-opening file: expected 1, got %d", readData[0])
	}
}

func TestDiskManager_RecoveryOnRestart(t *testing.T) {
	root := t.TempDir()

	dm1, _ := Initialize(root, 10)
	_ = dm1.CreateTableDir(TableID(42))

	data := make([]byte, PageSize)
	data[0] = 99
	_ = dm1.WritePage(TableID(42), PageID(1), data)
	dm1.Close()

	dm2, err := Initialize(root, 10)
	if err != nil {
		t.Fatalf("Failed to initialize on restart: %v", err)
	}
	defer dm2.Close()

	readData := make([]byte, PageSize)
	if err := dm2.ReadPage(TableID(42), PageID(1), readData); err != nil {
		t.Fatalf("Failed to read page after restart: %v", err)
	}

	if readData[0] != 99 {
		t.Errorf("Data corrupted after restart: expected 99, got %d", readData[0])
	}
}

func TestDiskManager_ReadUnwrittenPage(t *testing.T) {
	root := t.TempDir()
	dm, _ := Initialize(root, 10)
	defer dm.Close()

	tableID := TableID(1)
	_ = dm.CreateTableDir(tableID)

	readData := make([]byte, PageSize)
	err := dm.ReadPage(tableID, PageID(999), readData)
	if err != nil {
		t.Fatalf("Expected successful read with zero padding, got error: %v", err)
	}

	for i, b := range readData {
		if b != 0 {
			t.Errorf("Expected 0 padding at index %d, got %d", i, b)
			break
		}
	}
}
