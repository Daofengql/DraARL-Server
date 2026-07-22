package interconnect

import (
	"testing"
)

func TestProjectionSnapshotChunksAndCommits(t *testing.T) {
	p := NewProjection(7)
	p.Version = 12
	for i := 1; i <= 3000; i++ {
		p.Devices[uint64(i)] = DeviceRoute{SessionID: uint64(i), DeviceID: i, Username: "operator", DomainID: 3, SessionEpoch: 1}
	}
	begin, chunks, err := SplitProjection(99, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatal("expected chunked snapshot")
	}
	assembler, err := NewSnapshotAssembler(begin)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(chunks) - 1; i >= 0; i-- {
		if err := assembler.Add(chunks[i]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := assembler.Commit(SnapshotCommit{SnapshotID: 99})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != p.Version || len(got.Devices) != len(p.Devices) {
		t.Fatalf("snapshot mismatch: version=%d devices=%d", got.Version, len(got.Devices))
	}
}

func TestRelayFrameRoundTrip(t *testing.T) {
	inner := make([]byte, 90)
	copy(inner, "DraA")
	f := RelayFrame{SessionID: 1, SessionEpoch: 2, DomainID: 3, RequiredProjectionVersion: 4, InnerPacket: inner}
	wire, err := f.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalRelayFrame(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != f.SessionID || got.DomainID != f.DomainID || len(got.InnerPacket) != 90 {
		t.Fatalf("relay mismatch: %#v", got)
	}
}
