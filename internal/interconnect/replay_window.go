package interconnect

import "sync"

const (
	// 【重放窗口加固】窗口从 4096 扩大到 16384（4x），显著降低大跨度消息后
	// 整窗清空导致旧报文可重放的窗口；仍保留滑动窗口的下界拒绝语义。
	replayWindowBits  = 16384
	replayWindowWords = replayWindowBits / 64
)

// replayWindow accepts out-of-order IDs inside a fixed sliding window and
// rejects every ID at most once for the lifetime of a NodeSession. Sequential
// traffic clears one bit and sets one bit, avoiding the old per-packet map scan.
type replayWindow struct {
	mu      sync.Mutex
	maxID   uint64
	seenAny bool
	bits    [replayWindowWords]uint64
}

func (w *replayWindow) accept(messageID uint64) bool {
	if messageID == 0 {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.seenAny {
		w.seenAny = true
		w.maxID = messageID
		w.set(messageID)
		return true
	}
	if messageID > w.maxID {
		delta := messageID - w.maxID
		if delta >= replayWindowBits {
			clear(w.bits[:])
		} else {
			w.clearRange(w.maxID+1, delta)
		}
		w.maxID = messageID
	} else if w.maxID-messageID >= replayWindowBits {
		return false
	}
	if w.contains(messageID) {
		return false
	}
	w.set(messageID)
	return true
}

func (w *replayWindow) bit(messageID uint64) (int, uint64) {
	index := messageID & (replayWindowBits - 1)
	return int(index >> 6), uint64(1) << (index & 63)
}

func (w *replayWindow) contains(messageID uint64) bool {
	word, mask := w.bit(messageID)
	return w.bits[word]&mask != 0
}

func (w *replayWindow) set(messageID uint64) {
	word, mask := w.bit(messageID)
	w.bits[word] |= mask
}

// Clear only the newly entered positions, up to one word per iteration.
// The circular bitmap may wrap at the end of the window.
func (w *replayWindow) clearRange(first, count uint64) {
	index := first & (replayWindowBits - 1)
	for count > 0 {
		offset := index & 63
		length := min(count, 64-offset)
		mask := (^uint64(0) >> (64 - length)) << offset
		w.bits[index>>6] &^= mask
		count -= length
		index = (index + length) & (replayWindowBits - 1)
	}
}
