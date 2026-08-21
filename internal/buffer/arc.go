package buffer

import "container/list"

type ArcReplacer struct {
	mru      *list.List
	mfu      *list.List
	mruGhost *list.List
	mfuGhost *list.List

	mruTargetSize int
	mfuTargetSize int
}
