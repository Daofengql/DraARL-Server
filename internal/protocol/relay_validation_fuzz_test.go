package protocol

import "testing"

func FuzzValidateRelayInnerPacket(f *testing.F) {
	f.Add(EncodeDraARLv1("alice", "", 1, DraARLTypeTextMessage, 2, 7, "BG5ABC", []byte("text")))
	f.Add(EncodeDraARLv1("alice", "", 1, DraARLTypeOpus16K, 2, 7, "BG5ABC", []byte{1, 2, 3}))
	f.Add(EncodeDraARLv1("alice", "secret", 1, DraARLTypeOpus16K, 2, 7, "BG5ABC", nil))
	f.Add([]byte("DraA"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if ValidateRelayInnerPacket(data) != nil {
			return
		}
		_ = RelayInnerIdentityMatches(data, "alice", "BG5ABC", 1)
	})
}
