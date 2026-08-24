package buffer

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
	frame     *frame
	pageID    uint32
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
			pageInfo.evictable = t.Value.(pageMeta).evictable
			arc.mru.Remove(t)
			pageInfo.list = mfu
			elem := arc.mfu.PushFront(pageInfo)
			arc.inArc[pageID] = elem
			arc.mu.Unlock()

			return nil
		} else {
			pageInfo.evictable = t.Value.(pageMeta).evictable
			arc.mfu.Remove(t)
			pageInfo.list = mfu
			arc.mfu.PushFront(pageInfo)
			arc.mu.Unlock()

			return nil
		}
	case ok && t.Value.(pageMeta).list == mfuGhost:
		delta := max(1, arc.mruGhost.Len()/arc.mfuGhost.Len())
		arc.p = max(0, arc.p-uint32(delta))

		arc.mfuGhost.Remove(t)
		pageInfo.list = mfu
		elem := arc.mfu.PushFront(pageInfo)
		arc.inArc[pageID] = elem
		arc.mu.Unlock()
		return nil
	case ok && t.Value.(pageMeta).list == mruGhost:
		delta := max(1, arc.mfuGhost.Len()/arc.mruGhost.Len())
		arc.p = min(arc.framesNumber, arc.p+uint32(delta))

		arc.mruGhost.Remove(t)
		pageInfo.list = mfu
		elem := arc.mfu.PushFront(pageInfo)
		arc.inArc[pageID] = elem
		arc.mu.Unlock()
		return nil
	case !ok:
		pageInfo.list = mru
		elem := arc.mru.PushFront(pageInfo)
		arc.inArc[pageID] = elem
		arc.mu.Unlock()

		return nil
	}

	return nil
}

func (arc *ArcReplacer) replace() (pageMeta, error) {
	var result pageMeta
	if arc.mru.Len() > int(arc.p) {
		elemForReplace := arc.mru.Back()
		for {
			if elemForReplace != nil && elemForReplace.Value.(pageMeta).evictable {
				break
			}

			if elemForReplace != nil && !elemForReplace.Value.(pageMeta).evictable && elemForReplace.Prev() != nil {
				elemForReplace = elemForReplace.Prev()
				continue
			}

			if elemForReplace != nil && elemForReplace.Value.(pageMeta).list == mfu && elemForReplace.Prev() == nil {
				return pageMeta{}, errors.New("nullopt")
			}

			elemForReplace = arc.mfu.Back()
		}

		meta := elemForReplace.Value.(pageMeta)
		meta.frame = nil
		meta.list = mruGhost
		meta.evictable = true
		result = elemForReplace.Value.(pageMeta)
		arc.mru.Remove(elemForReplace)
		arc.inArc[meta.pageID] = arc.mruGhost.PushFront(meta)
	} else {
		elemForReplace := arc.mfu.Back()
		for {
			if elemForReplace != nil && elemForReplace.Value.(pageMeta).evictable {
				break
			}

			if elemForReplace != nil && !elemForReplace.Value.(pageMeta).evictable && elemForReplace.Prev() != nil {
				elemForReplace = elemForReplace.Prev()
				continue
			}

			if elemForReplace != nil && elemForReplace.Value.(pageMeta).list == mru && elemForReplace.Prev() == nil {
				return pageMeta{}, errors.New("nullopt")
			}

			elemForReplace = arc.mru.Back()
		}

		meta := elemForReplace.Value.(pageMeta)
		meta.frame = nil
		meta.list = mfuGhost
		meta.evictable = true
		result = elemForReplace.Value.(pageMeta)
		arc.mfu.Remove(elemForReplace)
		arc.inArc[meta.pageID] = arc.mfuGhost.PushFront(meta)
	}

	if (arc.mfuGhost.Len() + arc.mruGhost.Len()) > int(arc.framesNumber) {
		arc.removeGhost()
	}

	return result, nil
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

func (arc *ArcReplacer) evict() (frame *frame, suc bool) {
	arc.mu.Lock()
	defer arc.mu.Unlock()
	f, err := arc.replace()
	if err != nil {
		return nil, false
	}

	return f.frame, true
}

func (arc *ArcReplacer) size() int {
	arc.mu.Lock()
	counter := 0
	i := arc.mru.Back()
	for {
		if i == nil {
			break
		}

		if i.Value.(pageMeta).evictable {
			counter++
		}

		i = i.Prev()
	}

	i = arc.mfu.Back()
	for {
		if i == nil {
			break
		}

		if i.Value.(pageMeta).evictable {
			counter++
		}

		i = i.Prev()
	}

	arc.mu.Unlock()
	return counter
}

func (arc *ArcReplacer) remove(frame *frame) {
	arc.mu.Lock()
	defer arc.mu.Unlock()
	t, ok := arc.inArc[frame.pageID]
	if !ok {
		return
	}

	delete(arc.inArc, frame.pageID)
	switch t.Value.(pageMeta).list {
	case mru:
		arc.mru.Remove(t)
	case mfu:
		arc.mfu.Remove(t)
	case mruGhost:
		arc.mruGhost.Remove(t)
	case mfuGhost:
		arc.mfuGhost.Remove(t)
	}
}

func (arc *ArcReplacer) setEvictable(frame *frame, ev bool) error {
	arc.mu.Lock()
	defer arc.mu.Unlock()
	t, ok := arc.inArc[frame.pageID]
	if !ok {
		return errors.New("element not found")
	}

	meta := t.Value.(pageMeta)
	meta.evictable = ev
	t.Value = meta
	return nil
}
