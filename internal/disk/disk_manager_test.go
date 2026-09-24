package disk

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

func TestPageLocation(t *testing.T) {
	tests := []struct {
		pid         PageID
		expectedSeg uint32
		expectedOff int64
	}{
		{pid: 0, expectedSeg: 0, expectedOff: 0},
		{pid: 1, expectedSeg: 0, expectedOff: 8192},
		{pid: 8191, expectedSeg: 0, expectedOff: 67100672},
		{pid: 8192, expectedSeg: 1, expectedOff: 0},
		{pid: 8193, expectedSeg: 1, expectedOff: 8192},
	}

	for _, tt := range tests {
		seg, off := pageLocation(tt.pid)
		if seg != tt.expectedSeg || off != tt.expectedOff {
			t.Errorf("pageLocation(%d) = (%d, %d); want (%d, %d)",
				tt.pid, seg, off, tt.expectedSeg, tt.expectedOff)
		}
	}
}

func TestDiskManager_ReadPage(t *testing.T) {
	tmpRoot, err := os.MkdirTemp("", "db_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpRoot)

	dm, err := Initialize(tmpRoot)
	if err != nil {
		t.Fatalf("failed to initialize disk manager: %v", err)
	}

	var tableID TableID = 1
	tableDir := filepath.Join(dm.dataDir, "table_1")
	if err := os.MkdirAll(tableDir, 0o750); err != nil {
		t.Fatalf("failed to create table dir: %v", err)
	}

	dm.mu.Lock()
	dm.tables[tableID] = tableDir
	dm.mu.Unlock()

	segmentPath := filepath.Join(tableDir, "segment0")

	expectedData := make([]byte, PageSize)
	for i := range expectedData {
		expectedData[i] = byte(i % 256)
	}

	if err := os.WriteFile(segmentPath, expectedData, 0o640); err != nil {
		t.Fatalf("failed to write mock segment file: %v", err)
	}

	t.Run("Successful Read", func(t *testing.T) {
		data, err := dm.ReadPage(tableID, 0)
		if err != nil {
			t.Fatalf("unexpected error during ReadPage: %v", err)
		}

		if len(data) != PageSize {
			t.Fatalf("expected data size %d, got %d", PageSize, len(data))
		}

		if !bytes.Equal(data, expectedData) {
			t.Error("read data does not match expected data")
		}
	})

	t.Run("Invalid Table ID", func(t *testing.T) {
		var badTableID TableID = 999
		_, err := dm.ReadPage(badTableID, 0)
		if !errors.Is(err, ErrInvalidTableId) {
			t.Errorf("expected error %v, got %v", ErrInvalidTableId, err)
		}
	})

	t.Run("Missing Segment File", func(t *testing.T) {
		_, err := dm.ReadPage(tableID, 8192)
		if err == nil {
			t.Error("expected error for empty newly created file, got nil")
		}
	})
}

func TestMakeAlignedBuffer(t *testing.T) {
	buf, err := makeAlignedBuffer(PageSize)
	if err != nil {
		t.Fatalf("failed to allocate aligned buffer: %v", err)
	}
	defer syscall.Munmap(buf)

	if len(buf) != PageSize {
		t.Errorf("expected buffer size %d, got %d", PageSize, len(buf))
	}

	address := uintptr(unsafe.Pointer(&buf[0]))
	if address%4096 != 0 {
		t.Errorf("buffer is not 4096-byte aligned, address: %X", address)
	}
}
