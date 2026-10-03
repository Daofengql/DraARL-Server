package interconnect

import (
	"math/rand"
	"testing"
)

// Compare against a set-based reference across ring/word boundaries, jumps,
// stale packets and replays. The reference deliberately does not use bit masks.
func TestReplayWindowRandomizedReference(t *testing.T) {
	rng := rand.New(rand.NewSource(20261003))
	var window replayWindow
	seen := make(map[uint64]bool)
	var maximum uint64
	for i := 0; i < 30000; i++ {
		var id uint64
		switch rng.Intn(4) {
		case 0:
			id = maximum + uint64(rng.Intn(replayWindowBits*2)+1)
		case 1:
			id = maximum + 1
		default:
			age := uint64(rng.Intn(replayWindowBits * 2))
			if maximum > age {
				id = maximum - age
			}
		}
		want := id != 0 && (id > maximum || maximum-id < replayWindowBits) && !seen[id]
		if got := window.accept(id); got != want {
			t.Fatalf("step %d: id=%d maximum=%d got=%v want=%v", i, id, maximum, got, want)
		}
		if want {
			seen[id] = true
			if id > maximum {
				maximum = id
			}
		}
	}
}

func BenchmarkReplayWindowLargeJump(b *testing.B) {
	var window replayWindow
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		window.accept(uint64(i+1) * (replayWindowBits - 1))
	}
}
