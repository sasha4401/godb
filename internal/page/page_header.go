package page

type Slot struct {
	Offset uint16
	Size   uint16
}

type PageHeader struct {
	PageType       uint16
	TupleCount     uint16
	FreeSpaceStart uint16
	FreeSpaceEnd   uint16
	CheckSum       uint32
}
