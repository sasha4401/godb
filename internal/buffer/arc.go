package buffer

import (
	"container/list"
	"errors"
	"sync"

	"github.com/sasha4401/godb/internal/disk"
)

type state uint8

const (
	mru state = iota
	mfu
	mruGhost
	mfuGhost
)

type pageMeta struct {
	frameID   uint32
	pageID    disk.PageID
	list      state
	evictable bool
}

type ArcReplacer struct {
	mru      *list.List
	mfu      *list.List
	mruGhost *list.List
	mfuGhost *list.List

	p            uint32
	framesNumber uint32
	evictableNum uint32

	//map contains info about frames in cache, where key is ID page and value is elem cache
	inArc map[disk.PageID]*list.Element

	mu sync.RWMutex
}

func NewArcReplacer(framesNumber uint32) *ArcReplacer {
	p := framesNumber / 2
	inArc := make(map[disk.PageID]*list.Element)
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
		evictableNum: 0,
		inArc:        inArc,
		mu:           sync.RWMutex{},
	}
}

func (arc *ArcReplacer) recordAccess(frame uint32, pageID disk.PageID) error {
	pageInfo := pageMeta{
		frameID: frame,
		pageID:  pageID,
	}
	arc.mu.Lock()
	t, ok := arc.inArc[pageID]
	if !ok {
		pageInfo.list = mru
		elem := arc.mru.PushFront(pageInfo)
		arc.inArc[pageID] = elem
		arc.mu.Unlock()

		return nil
	}

	meta := t.Value.(pageMeta)
	switch {
	case meta.list == mru || meta.list == mfu:
		if meta.list == mru {
			pageInfo.evictable = meta.evictable
			arc.mru.Remove(t)
			pageInfo.list = mfu
			elem := arc.mfu.PushFront(pageInfo)
			arc.inArc[pageID] = elem
			arc.mu.Unlock()

			return nil
		} else {
			pageInfo.evictable = meta.evictable
			arc.mfu.Remove(t)
			pageInfo.list = mfu
			arc.mfu.PushFront(pageInfo)
			arc.mu.Unlock()

			return nil
		}
	case meta.list == mfuGhost:
		delta := max(1, arc.mruGhost.Len()/arc.mfuGhost.Len())
		arc.p = max(0, arc.p-uint32(delta))

		arc.mfuGhost.Remove(t)
		pageInfo.list = mfu
		elem := arc.mfu.PushFront(pageInfo)
		arc.inArc[pageID] = elem
		arc.mu.Unlock()
		return nil
	case meta.list == mruGhost:
		delta := max(1, arc.mfuGhost.Len()/arc.mruGhost.Len())
		arc.p = min(arc.framesNumber, arc.p+uint32(delta))

		arc.mruGhost.Remove(t)
		pageInfo.list = mfu
		elem := arc.mfu.PushFront(pageInfo)
		arc.inArc[pageID] = elem
		arc.mu.Unlock()
		return nil
	}

	return nil
}

func (arc *ArcReplacer) replace() (pageMeta, bool) {
	var result pageMeta
	var elemForReplace *list.Element
	if arc.mru.Len() > int(arc.p) {
		elemForReplace = arc.mru.Back()
		isJump := false
		for {
			if elemForReplace != nil && elemForReplace.Value.(pageMeta).evictable {
				break
			}

			if elemForReplace != nil && !elemForReplace.Value.(pageMeta).evictable && elemForReplace.Prev() != nil {
				elemForReplace = elemForReplace.Prev()
				continue
			}

			if elemForReplace == nil && isJump {
				return pageMeta{}, false
			}

			elemForReplace = arc.mfu.Back()
			isJump = true
		}
	} else {
		elemForReplace = arc.mfu.Back()
		isJump := false
		for {
			if elemForReplace != nil && elemForReplace.Value.(pageMeta).evictable {
				break
			}

			if elemForReplace != nil && !elemForReplace.Value.(pageMeta).evictable && elemForReplace.Prev() != nil {
				elemForReplace = elemForReplace.Prev()
				continue
			}

			if elemForReplace == nil && isJump {
				return pageMeta{}, false
			}

			elemForReplace = arc.mru.Back()
			isJump = true
		}
	}

	result = elemForReplace.Value.(pageMeta)
	meta := elemForReplace.Value.(pageMeta)
	meta.frameID = 0
	meta.evictable = true
	arc.evictableNum--
	switch elemForReplace.Value.(pageMeta).list {
	case mru:
		meta.list = mruGhost
		arc.mru.Remove(elemForReplace)
		arc.inArc[meta.pageID] = arc.mruGhost.PushFront(meta)
	case mfu:
		meta.list = mfuGhost
		arc.mfu.Remove(elemForReplace)
		arc.inArc[meta.pageID] = arc.mfuGhost.PushFront(meta)
	}

	if (arc.mfuGhost.Len() + arc.mruGhost.Len()) > int(arc.framesNumber) {
		arc.removeGhost()
	}

	return result, true
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

func (arc *ArcReplacer) evict() (frameID uint32, suc bool) {
	arc.mu.Lock()
	defer arc.mu.Unlock()
	f, flag := arc.replace()
	if !flag {
		return 0, false
	}

	return f.frameID, true
}

func (arc *ArcReplacer) size() int {
	arc.mu.RLock()
	defer arc.mu.RUnlock()
	return int(arc.evictableNum)
}

func (arc *ArcReplacer) remove(frame *frame) {
	arc.mu.Lock()
	defer arc.mu.Unlock()
	t, ok := arc.inArc[frame.pageID]
	if !ok {
		return
	}

	if !t.Value.(pageMeta).evictable {
		return
	}

	switch t.Value.(pageMeta).list {
	case mru:
		arc.mru.Remove(t)
	case mfu:
		arc.mfu.Remove(t)
	case mruGhost:
		return
	case mfuGhost:
		return
	}

	delete(arc.inArc, frame.pageID)
	arc.evictableNum--
}

func (arc *ArcReplacer) setEvictable(frame *frame, ev bool) error {
	arc.mu.Lock()
	defer arc.mu.Unlock()
	t, ok := arc.inArc[frame.pageID]
	if !ok {
		return errors.New("element not found")
	}

	meta := t.Value.(pageMeta)
	if meta.evictable != ev {
		if ev {
			arc.evictableNum++
		} else {
			arc.evictableNum--
		}
	}

	meta.evictable = ev
	t.Value = meta
	return nil
}
