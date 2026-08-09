package udphub

import (
	"encoding/binary"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newUDPTestPair(t *testing.T) (*net.UDPConn, *net.UDPConn, *net.UDPAddr) {
	t.Helper()
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen receiver: %v", err)
	}
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		receiver.Close()
		t.Fatalf("listen sender: %v", err)
	}
	t.Cleanup(func() {
		sender.Close()
		receiver.Close()
	})
	return sender, receiver, receiver.LocalAddr().(*net.UDPAddr)
}

func enqueueTestFrame(sender *FanoutSender, payload []byte, addr *net.UDPAddr) bool {
	ap, ok := udpAddrPort(addr)
	if !ok || sender == nil || len(sender.writers) == 0 {
		return false
	}
	entry := domainReceiverEntry{addr: ap}
	partitions := make([][]domainReceiverEntry, len(sender.writers))
	index := addrPortShard(ap, len(sender.writers))
	partitions[index] = append(partitions[index], entry)
	snap := &domainReceiverSnap{
		entries: []domainReceiverEntry{entry}, partitions: partitions,
		workers: len(sender.writers), gen: atomic.LoadUint64(&domainReceiverGen),
	}
	return sender.enqueueDomainFrame(payload, snap, 0, "", 0)
}

func TestFanoutSenderPreservesOrderPerAddress(t *testing.T) {
	senderConn, receiverConn, receiverAddr := newUDPTestPair(t)
	sender := newFanoutSender(senderConn, 4, 4096)
	defer sender.stop()

	const (
		packetCount = 1000
		batchSize   = 64
	)
	buf := make([]byte, 32)
	for batchStart := 0; batchStart < packetCount; batchStart += batchSize {
		batchEnd := min(batchStart+batchSize, packetCount)
		for seq := batchStart; seq < batchEnd; seq++ {
			payload := make([]byte, 4)
			binary.BigEndian.PutUint32(payload, uint32(seq))
			if !enqueueTestFrame(sender, payload, receiverAddr) {
				t.Fatalf("packet %d was not accepted", seq)
			}
		}

		if err := receiverConn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatalf("set deadline for batch at packet %d: %v", batchStart, err)
		}
		for want := batchStart; want < batchEnd; want++ {
			n, _, err := receiverConn.ReadFromUDP(buf)
			if err != nil {
				t.Fatalf("read packet %d: %v", want, err)
			}
			if n != 4 {
				t.Fatalf("packet %d length = %d, want 4", want, n)
			}
			if got := int(binary.BigEndian.Uint32(buf[:4])); got != want {
				t.Fatalf("packet order mismatch: got %d, want %d", got, want)
			}
		}
	}
}

func TestFanoutSenderConcurrentStop(t *testing.T) {
	senderConn, _, receiverAddr := newUDPTestPair(t)
	sender := newFanoutSender(senderConn, 4, 256)

	start := make(chan struct{})
	var producers sync.WaitGroup
	for i := 0; i < 8; i++ {
		producers.Add(1)
		go func(id int) {
			defer producers.Done()
			<-start
			payload := []byte{byte(id)}
			for j := 0; j < 2000; j++ {
				enqueueTestFrame(sender, payload, receiverAddr)
				if j%32 == 0 {
					runtime.Gosched()
				}
			}
		}(i)
	}

	close(start)
	runtime.Gosched()
	sender.stop()
	producers.Wait()

	if enqueueTestFrame(sender, []byte{1}, receiverAddr) {
		t.Fatal("stopped sender accepted a new frame")
	}
}

func TestFanoutSenderWritersKeepServerSourcePort(t *testing.T) {
	senderConn, receiverConn, receiverAddr := newUDPTestPair(t)
	sender := newFanoutSender(senderConn, 4, 64)
	defer sender.stop()

	if len(sender.writers) < 2 {
		t.Skip("platform did not provide duplicated UDP descriptors")
	}
	serverPort := senderConn.LocalAddr().(*net.UDPAddr).Port
	for index := range sender.writers {
		payload := []byte{byte(index)}
		if _, err := sender.writers[index].conn.WriteToUDP(payload, receiverAddr); err != nil {
			t.Fatalf("writer %d send: %v", index, err)
		}
	}

	if err := receiverConn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	buf := make([]byte, 8)
	for range sender.writers {
		_, source, err := receiverConn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("receive duplicated-writer packet: %v", err)
		}
		if source.Port != serverPort {
			t.Fatalf("source port = %d, want listener port %d", source.Port, serverPort)
		}
	}
}

func TestEnqueueLatestFrameEvictsWholeOldestFrame(t *testing.T) {
	queue := make(chan fanoutFrameJob, 2)
	queue <- fanoutFrameJob{data: []byte{1}}
	queue <- fanoutFrameJob{data: []byte{2}}

	accepted, evicted := enqueueLatestFrame(queue, fanoutFrameJob{data: []byte{3}})
	if !accepted || evicted == nil || evicted.data[0] != 1 {
		t.Fatalf("accepted=%v evicted=%v", accepted, evicted)
	}
	if got := (<-queue).data[0]; got != 2 {
		t.Fatalf("oldest retained frame = %d, want 2", got)
	}
	if got := (<-queue).data[0]; got != 3 {
		t.Fatalf("fresh frame = %d, want 3", got)
	}
}

func TestFanoutCompletionCountsOnlySuccessfulSocketWrites(t *testing.T) {
	senderConn, receiverConn, receiverAddr := newUDPTestPair(t)
	sender := newFanoutSender(senderConn, 2, 8)
	defer sender.stop()
	ap, ok := udpAddrPort(receiverAddr)
	if !ok {
		t.Fatal("receiver address is not usable")
	}
	partitions := make([][]domainReceiverEntry, len(sender.writers))
	partitions[addrPortShard(ap, len(sender.writers))] = []domainReceiverEntry{{addr: ap}}
	completed := make(chan fanoutWriteResult, 1)
	if !sender.enqueue(fanoutFrameJob{data: []byte{1, 2, 3}, partitions: partitions, enqueuedAt: time.Now(), onComplete: func(result fanoutWriteResult) { completed <- result }}) {
		t.Fatal("fan-out frame was rejected")
	}
	_ = receiverConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 8)
	if n, _, err := receiverConn.ReadFromUDP(buf); err != nil || n != 3 {
		t.Fatalf("receive fan-out payload: n=%d err=%v", n, err)
	}
	select {
	case result := <-completed:
		if result.attempted != 1 || result.sent != 1 || result.errors != 0 || result.dropped != 0 {
			t.Fatalf("unexpected completion result: %#v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fan-out completion was not reported")
	}
}

func TestStaleFanoutCompletionReportsDroppedTargets(t *testing.T) {
	senderConn, _, receiverAddr := newUDPTestPair(t)
	sender := newFanoutSenderWithMaxAge(senderConn, 2, 8, time.Millisecond)
	defer sender.stop()
	ap, ok := udpAddrPort(receiverAddr)
	if !ok {
		t.Fatal("receiver address is not usable")
	}
	partitions := make([][]domainReceiverEntry, len(sender.writers))
	partitions[addrPortShard(ap, len(sender.writers))] = []domainReceiverEntry{{addr: ap}}
	completed := make(chan fanoutWriteResult, 1)
	if !sender.enqueue(fanoutFrameJob{data: []byte{1}, partitions: partitions, enqueuedAt: time.Now().Add(-time.Second), onComplete: func(result fanoutWriteResult) { completed <- result }}) {
		t.Fatal("stale frame was not accepted for dispatcher accounting")
	}
	select {
	case result := <-completed:
		if result.sent != 0 || result.dropped != 1 {
			t.Fatalf("unexpected stale completion result: %#v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stale fan-out completion was not reported")
	}
}
