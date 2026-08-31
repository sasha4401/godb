package buffer

import "github.com/sasha4401/godb/internal/disk"

const PageSize = 8192

type frame struct {
	pageID   disk.PageID
	frameID  uint32
	pinCount uint32
	isDirty  bool
	data     [PageSize]byte
}
