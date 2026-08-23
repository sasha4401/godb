package buffer

//TODO Get a piece of Evict code from Read Access

import (
	"container/list"
	"errors"
	"sync"
)

type state uint8

const (
	mru state = iota
	mfu
	mruGhost
	mfuGhost
)

type pageMeta struct {
	frame  *frame
	pageID uint32
	list   state
}

type ArcReplacer struct {
	mru      *list.List
	mfu      *list.List
	mruGhost *list.List
	mfuGhost *list.List

	p            uint32
	framesNumber uint32

	//map contains info about frames in cache, where key is ID page and value is elem cache
	inArc map[uint32]*list.Element

	mu sync.Mutex
}

func NewArcReplacer(framesNumber uint32) *ArcReplacer {
	p := framesNumber / 2
	inArc := make(map[uint32]*list.Element)
	mru := list.New()
	mfu := list.New()
	mruGhost := list.New()
	mfuGhost := list.New()

	return &ArcReplacer{
		mru:          mru,
		mfu:          mfu,
		mruGhost:     mruGhost,
		mfuGhost:     mfuGhost,
		p:            p,
		framesNumber: framesNumber,
		inArc:        inArc,
		mu:           sync.Mutex{},
	}
}

func (arc *ArcReplacer) recordAccess(frame *frame) error {
	pageID := frame.pageID
	pageInfo := pageMeta{
		frame:  frame,
		pageID: pageID,
	}
	arc.mu.Lock()
	t, ok := arc.inArc[pageID]
	switch {
	case ok && (t.Value.(pageMeta).list == mru || t.Value.(pageMeta).list == mfu):
		if t.Value.(pageMeta).list == mru {
			arc.mru.Remove(t)
			pageInfo.list = mfu
			elem := arc.mfu.PushFront(pageInfo)
			arc.inArc[pageID] = elem
			arc.mu.Unlock()

			return nil
		} else {
			arc.mfu.Remove(t)
			pageInfo.list = mfu
			arc.mfu.PushFront(pageInfo)
			arc.mu.Unlock()

			return nil
		}
	case ok && t.Value.(pageMeta).list == mfuGhost:
		if arc.mruGhost.Len() <= arc.mfuGhost.Len() {
			if arc.p >= 1 {
				arc.p--
			}
		} else {
			sizeDiff := uint32(arc.mruGhost.Len()) / uint32(arc.mfuGhost.Len())
			if arc.p >= sizeDiff {
				arc.p -= (sizeDiff)
			}
		}

		arc.mfuGhost.Remove(t)
		pageInfo.list = mfu
		elem := arc.mfu.PushFront(pageInfo)
		arc.inArc[pageID] = elem
		if (arc.mfu.Len() + arc.mru.Len()) > int(arc.framesNumber) {
			for {
				err := arc.replace()
				if err == nil {
					break
				}
			}
		}
		arc.mu.Unlock()
		return nil
	case ok && t.Value.(pageMeta).list == mruGhost:
		if arc.mruGhost.Len() >= arc.mfuGhost.Len() {
			if arc.p <= arc.framesNumber-1 {
				arc.p++
			}
		} else {
			sizeDiff := uint32(arc.mfuGhost.Len()) / uint32(arc.mruGhost.Len())
			if arc.p <= arc.framesNumber-sizeDiff {
				arc.p += (sizeDiff)
			}
		}

		arc.mruGhost.Remove(t)
		pageInfo.list = mfu
		elem := arc.mfu.PushFront(pageInfo)
		arc.inArc[pageID] = elem
		if (arc.mfu.Len() + arc.mru.Len()) > int(arc.framesNumber) {
			for {
				err := arc.replace()
				if err == nil {
					break
				}
			}
		}
		arc.mu.Unlock()
		return nil
	case !ok:
		pageInfo.list = mru
		elem := arc.mru.PushFront(pageInfo)
		arc.inArc[pageID] = elem
		if (arc.mfu.Len() + arc.mru.Len()) > int(arc.framesNumber) {
			for {
				err := arc.replace()
				if err == nil {
					break
				}
			}
		}
		arc.mu.Unlock()

		return nil
	}

	return nil
}

func (arc *ArcReplacer) replace() error {
	if arc.mru.Len() > int(arc.p) {
		elemForReplace := arc.mru.Back()
		for {
			if elemForReplace.Value.(pageMeta).frame.pinCount == 0 {
				break
			}

			if elemForReplace.Value.(pageMeta).frame.pinCount > 0 && elemForReplace.Prev() != nil {
				elemForReplace = elemForReplace.Prev()
				continue
			}

			if elemForReplace.Value.(pageMeta).list == mfu && elemForReplace.Prev() == nil {
				return errors.New("nullopt")
			}

			elemForReplace = arc.mfu.Back()
		}

		meta := elemForReplace.Value.(pageMeta)
		meta.frame = nil
		meta.list = mruGhost
		arc.mru.Remove(elemForReplace)
		arc.inArc[meta.pageID] = arc.mruGhost.PushFront(meta)
	} else {
		elemForReplace := arc.mfu.Back()
		for {
			if elemForReplace.Value.(pageMeta).frame.pinCount == 0 {
				break
			}

			if elemForReplace.Value.(pageMeta).frame.pinCount > 0 && elemForReplace.Prev() != nil {
				elemForReplace = elemForReplace.Prev()
				continue
			}

			if elemForReplace.Value.(pageMeta).list == mru && elemForReplace.Prev() == nil {
				return errors.New("nullopt")
			}

			elemForReplace = arc.mru.Back()
		}

		meta := elemForReplace.Value.(pageMeta)
		meta.frame = nil
		meta.list = mfuGhost
		arc.mfu.Remove(elemForReplace)
		arc.inArc[meta.pageID] = arc.mfuGhost.PushFront(meta)
	}

	if (arc.mfuGhost.Len() + arc.mruGhost.Len()) > int(arc.framesNumber) {
		arc.removeGhost()
	}

	return nil
}

func (arc *ArcReplacer) removeGhost() {
	if arc.mruGhost.Len() > int(arc.p) {
		elemForDel := arc.mruGhost.Back()
		res := arc.mruGhost.Remove(elemForDel)
		delete(arc.inArc, res.(pageMeta).pageID)
	} else {
		elemForDel := arc.mfuGhost.Back()
		res := arc.mfuGhost.Remove(elemForDel)
		delete(arc.inArc, res.(pageMeta).pageID)
	}
}
