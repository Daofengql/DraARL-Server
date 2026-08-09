package interconnect

import "sync"

const (
	replayWindowBits  = 4096
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
			for step := uint64(1); step <= delta; step++ {
				w.clear(w.maxID + step)
			}
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

func (w *replayWindow) clear(messageID uint64) {
	word, mask := w.bit(messageID)
	w.bits[word] &^= mask
}
