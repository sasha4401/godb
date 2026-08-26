package buffer

import (
	"testing"
	"time"
)

func newTestFrame(pageID uint32) *frame {
	return &frame{
		pageID: pageID,
	}
}

func TestArcReplacer_RecordAccess(t *testing.T) {
	arc := NewArcReplacer(3)

	err := arc.recordAccess(1, 100)
	if err != nil {
		t.Fatalf("recordAccess failed: %v", err)
	}

	if arc.mru.Len() != 1 {
		t.Fatalf("expected MRU size 1, got %d", arc.mru.Len())
	}

	if arc.mfu.Len() != 0 {
		t.Fatalf("expected MFU size 0, got %d", arc.mfu.Len())
	}

	elem, ok := arc.inArc[100]
	if !ok {
		t.Fatal("page 100 was not added to inArc")
	}

	meta := elem.Value.(pageMeta)

	if meta.pageID != 100 {
		t.Fatalf("expected pageID 100, got %d", meta.pageID)
	}

	if meta.frameID != 1 {
		t.Fatalf("expected frameID 1, got %d", meta.frameID)
	}

	if meta.list != mru {
		t.Fatalf("expected page to be in MRU")
	}

	if meta.evictable {
		t.Fatal("new page should not be evictable")
	}
}

func TestArcReplacer_RecordAccessMovesMRUToMFU(t *testing.T) {
	arc := NewArcReplacer(3)

	if err := arc.recordAccess(1, 100); err != nil {
		t.Fatal(err)
	}

	if err := arc.recordAccess(1, 100); err != nil {
		t.Fatal(err)
	}

	if arc.mru.Len() != 0 {
		t.Fatalf("expected MRU size 0, got %d", arc.mru.Len())
	}

	if arc.mfu.Len() != 1 {
		t.Fatalf("expected MFU size 1, got %d", arc.mfu.Len())
	}

	elem, ok := arc.inArc[100]
	if !ok {
		t.Fatal("page 100 not found in inArc")
	}

	meta := elem.Value.(pageMeta)

	if meta.list != mfu {
		t.Fatalf("expected page to be in MFU")
	}

	if meta.frameID != 1 {
		t.Fatalf("expected frameID 1, got %d", meta.frameID)
	}
}

func TestArcReplacer_SetEvictable(t *testing.T) {
	arc := NewArcReplacer(3)

	f := newTestFrame(100)

	if err := arc.recordAccess(1, 100); err != nil {
		t.Fatal(err)
	}

	if got := arc.size(); got != 0 {
		t.Fatalf("expected size 0, got %d", got)
	}

	if err := arc.setEvictable(f, true); err != nil {
		t.Fatalf("setEvictable failed: %v", err)
	}

	if got := arc.size(); got != 1 {
		t.Fatalf("expected size 1, got %d", got)
	}

	elem := arc.inArc[100]
	meta := elem.Value.(pageMeta)

	if !meta.evictable {
		t.Fatal("expected frame to be evictable")
	}
}

func TestArcReplacer_SetEvictableFalse(t *testing.T) {
	arc := NewArcReplacer(3)

	f := newTestFrame(100)

	if err := arc.recordAccess(1, 100); err != nil {
		t.Fatal(err)
	}

	if err := arc.setEvictable(f, true); err != nil {
		t.Fatal(err)
	}

	if got := arc.size(); got != 1 {
		t.Fatalf("expected size 1, got %d", got)
	}

	if err := arc.setEvictable(f, false); err != nil {
		t.Fatal(err)
	}

	if got := arc.size(); got != 0 {
		t.Fatalf("expected size 0, got %d", got)
	}

	meta := arc.inArc[100].Value.(pageMeta)

	if meta.evictable {
		t.Fatal("expected frame to be non-evictable")
	}
}

func TestArcReplacer_SetEvictableRepeated(t *testing.T) {
	arc := NewArcReplacer(3)

	f := newTestFrame(100)

	if err := arc.recordAccess(1, 100); err != nil {
		t.Fatal(err)
	}

	if err := arc.setEvictable(f, true); err != nil {
		t.Fatal(err)
	}

	if err := arc.setEvictable(f, true); err != nil {
		t.Fatal(err)
	}

	if got := arc.size(); got != 1 {
		t.Fatalf("expected size 1, got %d", got)
	}

	if err := arc.setEvictable(f, false); err != nil {
		t.Fatal(err)
	}

	if err := arc.setEvictable(f, false); err != nil {
		t.Fatal(err)
	}

	if got := arc.size(); got != 0 {
		t.Fatalf("expected size 0, got %d", got)
	}
}

func TestArcReplacer_SetEvictableUnknownFrame(t *testing.T) {
	arc := NewArcReplacer(3)

	f := newTestFrame(999)

	err := arc.setEvictable(f, true)

	if err == nil {
		t.Fatal("expected error for unknown frame")
	}

	if got := arc.size(); got != 0 {
		t.Fatalf("expected size 0, got %d", got)
	}
}

func TestArcReplacer_Size(t *testing.T) {
	arc := NewArcReplacer(3)

	f0 := newTestFrame(100)
	f1 := newTestFrame(200)
	f2 := newTestFrame(300)

	if err := arc.recordAccess(3, 100); err != nil {
		t.Fatal(err)
	}

	if err := arc.recordAccess(1, 200); err != nil {
		t.Fatal(err)
	}

	if err := arc.recordAccess(2, 300); err != nil {
		t.Fatal(err)
	}

	if got := arc.size(); got != 0 {
		t.Fatalf("expected size 0, got %d", got)
	}

	if err := arc.setEvictable(f0, true); err != nil {
		t.Fatal(err)
	}

	if got := arc.size(); got != 1 {
		t.Fatalf("expected size 1, got %d", got)
	}

	if err := arc.setEvictable(f1, true); err != nil {
		t.Fatal(err)
	}

	if got := arc.size(); got != 2 {
		t.Fatalf("expected size 2, got %d", got)
	}

	if err := arc.setEvictable(f2, true); err != nil {
		t.Fatal(err)
	}

	if got := arc.size(); got != 3 {
		t.Fatalf("expected size 3, got %d", got)
	}
}

func TestArcReplacer_EvictEmpty(t *testing.T) {
	arc := NewArcReplacer(4)

	frameID, ok := arc.evict()

	if ok {
		t.Fatal("expected eviction to fail")
	}

	if frameID != 0 {
		t.Fatalf("expected frameID 0, got %d", frameID)
	}

	if got := arc.size(); got != 0 {
		t.Fatalf("expected size 0, got %d", got)
	}
}

func TestArcReplacer_Evict(t *testing.T) {
	arc := NewArcReplacer(3)

	f := newTestFrame(100)

	if err := arc.recordAccess(1, 100); err != nil {
		t.Fatal(err)
	}

	if err := arc.setEvictable(f, true); err != nil {
		t.Fatal(err)
	}

	if got := arc.size(); got != 1 {
		t.Fatalf("expected size 1, got %d", got)
	}

	frameID, ok := arc.evict()

	if !ok {
		t.Fatal("expected eviction to succeed")
	}

	if frameID != 1 {
		t.Fatalf("expected frameID 1, got %d", frameID)
	}

	if got := arc.size(); got != 0 {
		t.Fatalf("expected size 0 after eviction, got %d", got)
	}

	if arc.mruGhost.Len() != 1 {
		t.Fatalf("expected MRU ghost size 1, got %d", arc.mruGhost.Len())
	}
}

func TestArcReplacer_EvictNonEvictable(t *testing.T) {
	arc := NewArcReplacer(3)

	f0 := newTestFrame(100)
	f1 := newTestFrame(200)

	if err := arc.recordAccess(2, 100); err != nil {
		t.Fatal(err)
	}

	if err := arc.recordAccess(1, 200); err != nil {
		t.Fatal(err)
	}

	if err := arc.setEvictable(f1, true); err != nil {
		t.Fatal(err)
	}

	frameID, ok := arc.evict()

	if !ok {
		t.Fatal("expected eviction to succeed")
	}

	if frameID != 1 {
		t.Fatalf("expected frameID 1, got %d", frameID)
	}

	if _, ok := arc.inArc[100]; !ok {
		t.Fatal("non-evictable frame 0 was removed")
	}

	elem, ok := arc.inArc[200]
	if !ok {
		t.Fatal("evicted page 200 should remain in ghost list")
	}

	if elem.Value.(pageMeta).list != mruGhost {
		t.Fatal("expected page 200 to be in MRU ghost")
	}

	_ = f0
}

func TestArcReplacer_Remove(t *testing.T) {
	arc := NewArcReplacer(3)

	f := newTestFrame(100)

	if err := arc.recordAccess(1, 100); err != nil {
		t.Fatal(err)
	}

	if err := arc.setEvictable(f, true); err != nil {
		t.Fatal(err)
	}

	if got := arc.size(); got != 1 {
		t.Fatalf("expected size 1, got %d", got)
	}

	arc.remove(f)

	if got := arc.size(); got != 0 {
		t.Fatalf("expected size 0 after remove, got %d", got)
	}

	if _, ok := arc.inArc[100]; ok {
		t.Fatal("page 100 should have been removed from inArc")
	}

	if arc.mru.Len() != 0 {
		t.Fatalf("expected MRU size 0, got %d", arc.mru.Len())
	}
}

func TestArcReplacer_RemoveUnknown(t *testing.T) {
	arc := NewArcReplacer(3)

	f := newTestFrame(999)

	arc.remove(f)

	if got := arc.size(); got != 0 {
		t.Fatalf("expected size 0, got %d", got)
	}
}

func TestArcReplacer_RemoveNonEvictable(t *testing.T) {
	arc := NewArcReplacer(3)

	f := newTestFrame(100)

	if err := arc.recordAccess(0, 100); err != nil {
		t.Fatal(err)
	}

	arc.remove(f)

	if _, ok := arc.inArc[100]; !ok {
		t.Fatal("non-evictable frame was removed")
	}

	if arc.mru.Len() != 1 {
		t.Fatalf("expected MRU size 1, got %d", arc.mru.Len())
	}

	if got := arc.size(); got != 0 {
		t.Fatalf("expected size 0, got %d", got)
	}
}

func TestArcReplacer_GhostHit(t *testing.T) {
	arc := NewArcReplacer(2)

	f := newTestFrame(100)

	if err := arc.recordAccess(1, 100); err != nil {
		t.Fatal(err)
	}

	if err := arc.setEvictable(f, true); err != nil {
		t.Fatal(err)
	}

	_, ok := arc.evict()

	if !ok {
		t.Fatal("expected eviction to succeed")
	}

	if arc.mruGhost.Len() != 1 {
		t.Fatalf("expected MRU ghost size 1, got %d", arc.mruGhost.Len())
	}

	if err := arc.recordAccess(1, 100); err != nil {
		t.Fatal(err)
	}

	if arc.mruGhost.Len() != 0 {
		t.Fatalf("expected MRU ghost size 0, got %d", arc.mruGhost.Len())
	}

	if arc.mfu.Len() != 1 {
		t.Fatalf("expected MFU size 1, got %d", arc.mfu.Len())
	}

	elem, ok := arc.inArc[100]
	if !ok {
		t.Fatal("page 100 not found")
	}

	meta := elem.Value.(pageMeta)

	if meta.list != mfu {
		t.Fatalf("expected page 100 in MFU, got %v", meta.list)
	}

	if meta.frameID != 1 {
		t.Fatalf("expected frameID 0, got %d", meta.frameID)
	}
}

func TestArcReplacer_RemoveBehaviorTest(t *testing.T) {
	{
		arc := NewArcReplacer(5)

		arc.remove(newTestFrame(0))

		if got := arc.size(); got != 0 {
			t.Fatalf("expected size 0, got %d", got)
		}
	}

	{
		arc := NewArcReplacer(5)

		if err := arc.recordAccess(0, 10); err != nil {
			t.Fatal(err)
		}

		if err := arc.setEvictable(newTestFrame(10), true); err != nil {
			t.Fatal(err)
		}

		if got := arc.size(); got != 1 {
			t.Fatalf("expected size 1, got %d", got)
		}

		arc.remove(newTestFrame(99))

		if got := arc.size(); got != 1 {
			t.Fatalf("expected size 1, got %d", got)
		}
	}

	{
		arc := NewArcReplacer(5)

		for i := uint32(0); i < 3; i++ {
			if err := arc.recordAccess(i, 10+i); err != nil {
				t.Fatal(err)
			}

			if err := arc.setEvictable(newTestFrame(10+i), true); err != nil {
				t.Fatal(err)
			}
		}

		if got := arc.size(); got != 3 {
			t.Fatalf("expected size 3, got %d", got)
		}

		arc.remove(newTestFrame(11))

		if got := arc.size(); got != 2 {
			t.Fatalf("expected size 2, got %d", got)
		}

		if frameID, ok := arc.evict(); !ok || frameID != 0 {
			t.Fatalf("expected frame 0, got frame=%d ok=%v", frameID, ok)
		}

		if frameID, ok := arc.evict(); !ok || frameID != 2 {
			t.Fatalf("expected frame 2, got frame=%d ok=%v", frameID, ok)
		}

		if _, ok := arc.evict(); ok {
			t.Fatal("expected replacer to be empty")
		}
	}

	{
		arc := NewArcReplacer(4)

		if err := arc.recordAccess(0, 20); err != nil {
			t.Fatal(err)
		}

		if err := arc.setEvictable(newTestFrame(20), true); err != nil {
			t.Fatal(err)
		}

		if err := arc.recordAccess(1, 21); err != nil {
			t.Fatal(err)
		}

		if err := arc.setEvictable(newTestFrame(21), true); err != nil {
			t.Fatal(err)
		}

		if err := arc.recordAccess(0, 20); err != nil {
			t.Fatal(err)
		}

		if got := arc.size(); got != 2 {
			t.Fatalf("expected size 2, got %d", got)
		}

		arc.remove(newTestFrame(20))

		if got := arc.size(); got != 1 {
			t.Fatalf("expected size 1, got %d", got)
		}

		if frameID, ok := arc.evict(); !ok || frameID != 1 {
			t.Fatalf("expected frame 1, got frame=%d ok=%v", frameID, ok)
		}

		if _, ok := arc.evict(); ok {
			t.Fatal("expected replacer to be empty")
		}
	}

	{
		arc := NewArcReplacer(3)

		for i := uint32(0); i < 3; i++ {
			if err := arc.recordAccess(i, 30+i); err != nil {
				t.Fatal(err)
			}

			if err := arc.setEvictable(newTestFrame(30+i), true); err != nil {
				t.Fatal(err)
			}
		}

		arc.remove(newTestFrame(30))

		if got := arc.size(); got != 2 {
			t.Fatalf("expected size 2, got %d", got)
		}

		if err := arc.recordAccess(0, 30); err != nil {
			t.Fatal(err)
		}

		if err := arc.setEvictable(newTestFrame(30), true); err != nil {
			t.Fatal(err)
		}

		if got := arc.size(); got != 3 {
			t.Fatalf("expected size 3, got %d", got)
		}

		if frameID, ok := arc.evict(); !ok || frameID != 1 {
			t.Fatalf("expected frame 1, got frame=%d ok=%v", frameID, ok)
		}

		if frameID, ok := arc.evict(); !ok || frameID != 2 {
			t.Fatalf("expected frame 2, got frame=%d ok=%v", frameID, ok)
		}

		if frameID, ok := arc.evict(); !ok || frameID != 0 {
			t.Fatalf("expected frame 0, got frame=%d ok=%v", frameID, ok)
		}
	}

	{
		arc := NewArcReplacer(3)

		if err := arc.recordAccess(0, 40); err != nil {
			t.Fatal(err)
		}

		if err := arc.setEvictable(newTestFrame(40), true); err != nil {
			t.Fatal(err)
		}

		if got := arc.size(); got != 1 {
			t.Fatalf("expected size 1, got %d", got)
		}

		arc.remove(newTestFrame(40))

		if got := arc.size(); got != 0 {
			t.Fatalf("expected size 0, got %d", got)
		}

		arc.remove(newTestFrame(40))

		if got := arc.size(); got != 0 {
			t.Fatalf("expected size 0, got %d", got)
		}
	}

	{
		arc := NewArcReplacer(3)

		if err := arc.recordAccess(0, 50); err != nil {
			t.Fatal(err)
		}

		if err := arc.setEvictable(newTestFrame(50), true); err != nil {
			t.Fatal(err)
		}

		arc.remove(newTestFrame(50))

		if got := arc.size(); got != 0 {
			t.Fatalf("expected size 0, got %d", got)
		}

		if err := arc.recordAccess(0, 51); err != nil {
			t.Fatal(err)
		}

		if err := arc.setEvictable(newTestFrame(51), true); err != nil {
			t.Fatal(err)
		}

		if got := arc.size(); got != 1 {
			t.Fatalf("expected size 1, got %d", got)
		}

		if frameID, ok := arc.evict(); !ok || frameID != 0 {
			t.Fatalf("expected frame 0, got frame=%d ok=%v", frameID, ok)
		}
	}

	{
		arc := NewArcReplacer(5)

		for i := uint32(0); i < 5; i++ {
			if err := arc.recordAccess(i, 60+i); err != nil {
				t.Fatal(err)
			}

			if err := arc.setEvictable(newTestFrame(60+i), true); err != nil {
				t.Fatal(err)
			}
		}

		arc.remove(newTestFrame(62))

		if got := arc.size(); got != 4 {
			t.Fatalf("expected size 4, got %d", got)
		}

		expected := []uint32{0, 1, 3, 4}

		for _, want := range expected {
			if frameID, ok := arc.evict(); !ok || frameID != want {
				t.Fatalf("expected frame %d, got frame=%d ok=%v", want, frameID, ok)
			}
		}

		if _, ok := arc.evict(); ok {
			t.Fatal("expected replacer to be empty")
		}
	}

	{
		arc := NewArcReplacer(5)

		for i := uint32(0); i < 5; i++ {
			if err := arc.recordAccess(i, 80+i); err != nil {
				t.Fatal(err)
			}

			if err := arc.setEvictable(newTestFrame(80+i), true); err != nil {
				t.Fatal(err)
			}
		}

		if got := arc.size(); got != 5 {
			t.Fatalf("expected size 5, got %d", got)
		}

		for i := uint32(0); i < 5; i++ {
			arc.remove(newTestFrame(80 + i))
		}

		if got := arc.size(); got != 0 {
			t.Fatalf("expected size 0, got %d", got)
		}

		if _, ok := arc.evict(); ok {
			t.Fatal("expected replacer to be empty")
		}
	}
}

func TestArcReplacer_RecordAccessPerformanceTest(t *testing.T) {
	const bpmSize uint32 = 256 << 10

	arc := NewArcReplacer(bpmSize)

	for i := uint32(0); i < bpmSize; i++ {
		if err := arc.recordAccess(i, i); err != nil {
			t.Fatal(err)
		}

		if err := arc.setEvictable(newTestFrame(i), true); err != nil {
			t.Fatal(err)
		}
	}

	const rounds = 10
	var accessFrameID uint32 = 256 << 9

	accessTimes := make([]time.Duration, 0, rounds)

	for round := 0; round < rounds; round++ {
		start := time.Now()

		for i := uint32(0); i < bpmSize; i++ {
			if err := arc.recordAccess(accessFrameID, accessFrameID); err != nil {
				t.Fatal(err)
			}

			accessFrameID = (accessFrameID + 1) % bpmSize
		}

		accessTimes = append(accessTimes, time.Since(start))
	}

	var total time.Duration

	for _, duration := range accessTimes {
		total += duration
	}

	avg := total / rounds

	t.Logf("average RecordAccess time: %v", avg)

	if avg >= 3*time.Second {
		t.Fatalf("RecordAccess is too slow: average=%v", avg)
	}
}
