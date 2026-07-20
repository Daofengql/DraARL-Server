package udphub

import (
	"net"
	"net/netip"
)

func udpAddrPort(addr *net.UDPAddr) (netip.AddrPort, bool) {
	if addr == nil {
		return netip.AddrPort{}, false
	}
	ap := addr.AddrPort()
	if !ap.IsValid() {
		return netip.AddrPort{}, false
	}
	return ap, true
}

func hashAddrPort(ap netip.AddrPort) uint64 {
	if !ap.IsValid() {
		return 0
	}

	addr := ap.Addr()
	if addr.Is4In6() {
		addr = addr.Unmap()
	}

	const offset64 = uint64(1469598103934665603)
	const prime64 = uint64(1099511628211)
	h := offset64
	if addr.Is4() {
		b := addr.As4()
		for _, value := range b {
			h ^= uint64(value)
			h *= prime64
		}
	} else {
		b := addr.As16()
		for _, value := range b {
			h ^= uint64(value)
			h *= prime64
		}
	}
	for i := 0; i < len(addr.Zone()); i++ {
		h ^= uint64(addr.Zone()[i])
		h *= prime64
	}
	h ^= uint64(ap.Port() >> 8)
	h *= prime64
	h ^= uint64(byte(ap.Port()))
	h *= prime64
	return h
}

func udpAddrShard(addr *net.UDPAddr, shards int) int {
	if shards <= 1 {
		return 0
	}
	ap, ok := udpAddrPort(addr)
	if !ok {
		return 0
	}
	return int(hashAddrPort(ap) % uint64(shards))
}

func addrPortShard(ap netip.AddrPort, shards int) int {
	if shards <= 1 {
		return 0
	}
	return int(hashAddrPort(ap) % uint64(shards))
}
