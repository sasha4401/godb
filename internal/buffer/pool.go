package buffer

const PageSize = 8192

type frame struct {
	pageID   uint32
	frameID  uint32
	pinCount uint32
	isDirty  bool
	data     [PageSize]byte
}
