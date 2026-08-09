package protocol

import (
	"encoding/binary"
	"errors"
)

// ValidateRelayInnerPacket validates the ordinary packet carried by a Type 0
// relay without allocating header strings on the realtime path.
func ValidateRelayInnerPacket(data []byte) error {
	if len(data) < DraARLv1HeaderSize || len(data) > DraARLv1MaxPacketSize {
		return errors.New("invalid relay inner packet size")
	}
	if data[0] != 'D' || data[1] != 'r' || data[2] != 'a' || data[3] != 'A' || int(binary.BigEndian.Uint16(data[4:6])) != len(data) {
		return errors.New("invalid relay inner packet envelope")
	}
	if data[48] != DraARLTypeTextMessage && data[48] != DraARLTypeOpus16K {
		return errors.New("relay inner packet type is not realtime")
	}
	for _, value := range data[38:48] {
		if value != 0 {
			return errors.New("relay inner packet contains credentials")
		}
	}
	return nil
}

// RelayInnerIdentityMatches checks the centre-authoritative identity while
// retaining compatibility with compact device packets whose callsign field
// was left empty before the edge rewrote the forwarding header.
func RelayInnerIdentityMatches(data []byte, username, callSign string, ssid byte) bool {
	if len(data) < DraARLv1HeaderSize || data[50] != ssid || !fixedFieldMatches(data[6:38], username, false) {
		return false
	}
	return fixedFieldMatches(data[54:86], callSign, true)
}

func fixedFieldMatches(field []byte, expected string, allowEmpty bool) bool {
	if len(expected) > len(field) {
		return false
	}
	if allowEmpty {
		empty := true
		for _, value := range field {
			if value != 0 {
				empty = false
				break
			}
		}
		if empty {
			return true
		}
	}
	for index := range expected {
		if field[index] != expected[index] {
			return false
		}
	}
	for _, value := range field[len(expected):] {
		if value != 0 {
			return false
		}
	}
	return true
}
