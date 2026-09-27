package disk

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
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

	t.Run("Successful Read Existing Page", func(t *testing.T) {
		data := make([]byte, PageSize)
		err := dm.ReadPage(tableID, 0, data)
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
		data := make([]byte, PageSize)
		err := dm.ReadPage(badTableID, 0, data)
		if !errors.Is(err, ErrInvalidTableID) {
			t.Errorf("expected error %v, got %v", ErrInvalidTableID, err)
		}
	})

	t.Run("Read Non-Existent Page (New Page Scenario)", func(t *testing.T) {
		data := make([]byte, PageSize)
		for i := range data {
			data[i] = 0xFF
		}

		err := dm.ReadPage(tableID, 8192, data)
		if err != nil {
			t.Fatalf("unexpected error for missing segment file: %v", err)
		}

		zeroPage := make([]byte, PageSize)
		if !bytes.Equal(data, zeroPage) {
			t.Error("expected non-existent page to be filled with zeros")
		}
	})

	t.Run("Read Partially Written or Empty Existing Segment", func(t *testing.T) {
		emptySegmentPath := filepath.Join(tableDir, "segment2")
		if err := os.WriteFile(emptySegmentPath, []byte{}, 0o640); err != nil {
			t.Fatalf("failed to create empty segment file: %v", err)
		}

		data := make([]byte, PageSize)
		for i := range data {
			data[i] = 0xAA
		}

		err := dm.ReadPage(tableID, 16384, data)
		if err != nil {
			t.Fatalf("unexpected error for empty file: %v", err)
		}

		zeroPage := make([]byte, PageSize)
		if !bytes.Equal(data, zeroPage) {
			t.Error("expected frame to be zeroed when reading from empty file")
		}
	})
}

func TestDiskManager_WritePage(t *testing.T) {
	tmpRoot, err := os.MkdirTemp("", "db_write_test_*")
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

	t.Run("Successful Write and Read Back", func(t *testing.T) {
		writeFrame := make([]byte, PageSize)
		for i := range writeFrame {
			writeFrame[i] = byte(i % 128)
		}

		var pid PageID = 5
		err := dm.WritePage(tableID, pid, writeFrame)
		if err != nil {
			t.Fatalf("unexpected error during WritePage: %v", err)
		}

		readFrame := make([]byte, PageSize)
		err = dm.ReadPage(tableID, pid, readFrame)
		if err != nil {
			t.Fatalf("unexpected error during ReadPage: %v", err)
		}

		if !bytes.Equal(readFrame, writeFrame) {
			t.Error("read data does not match the written data")
		}
	})

	t.Run("Write Invalid Data Size", func(t *testing.T) {
		invalidFrame := make([]byte, PageSize-10)
		err := dm.WritePage(tableID, 0, invalidFrame)
		if !errors.Is(err, ErrInvalidData) {
			t.Errorf("expected error %v, got %v", ErrInvalidData, err)
		}
	})

	t.Run("Write Invalid Table ID", func(t *testing.T) {
		var badTableID TableID = 888
		frame := make([]byte, PageSize)
		err := dm.WritePage(badTableID, 0, frame)
		if !errors.Is(err, ErrInvalidTableID) {
			t.Errorf("expected error %v, got %v", ErrInvalidTableID, err)
		}
	})
}
