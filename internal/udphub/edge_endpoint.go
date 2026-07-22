package udphub

import (
	"errors"
	"net"
	"net/netip"
	"sync"
	"time"
)

// EdgeFanoutTarget is the minimal receiver identity required by the existing
// parallel fan-out engine. It contains no database or authentication state.
type EdgeFanoutTarget struct {
	Addr     *net.UDPAddr
	DeviceID int
	Username string
	SSID     byte
}

type EdgeFanoutResult struct {
	Attempted int64
	Sent      int64
	Dropped   int64
	Errors    int64
}

// EdgeEndpoint is the database-free form of the udphub UDP ingress. One
// socket carries both ordinary device packets and authenticated Type 0 node
// packets; callers distinguish them in handler. It intentionally reuses the
// same single-reader, sharded-worker and parallel fan-out design as the
// centre's udphub.
type EdgeEndpoint struct {
	conn      *net.UDPConn
	handler   func([]byte, *net.UDPAddr)
	queues    []chan udpDatagramJob
	readerWG  sync.WaitGroup
	workerWG  sync.WaitGroup
	closeOnce sync.Once
	closed    chan struct{}
	sender    *FanoutSender
}

func NewEdgeEndpoint(listenAddr string, handler func([]byte, *net.UDPAddr)) (*EdgeEndpoint, error) {
	if handler == nil {
		return nil, errors.New("edge UDP handler is required")
	}
	if listenAddr == "" {
		listenAddr = ":60050"
	}
	addr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	configureUDPSocketBuffers(conn)

	workers := udpWorkerCount()
	perQueue := udpJobQueueSize / workers
	if perQueue < 64 {
		perQueue = 64
	}
	e := &EdgeEndpoint{conn: conn, handler: handler, queues: make([]chan udpDatagramJob, workers), closed: make(chan struct{})}
	for i := range e.queues {
		e.queues[i] = make(chan udpDatagramJob, perQueue)
		e.workerWG.Add(1)
		go e.worker(e.queues[i])
	}
	queueSize, maxAge := fanoutRuntimeSettings()
	e.sender = newFanoutSenderWithMaxAge(conn, fanoutWorkerCount(), queueSize, maxAge)
	e.readerWG.Add(1)
	go e.readLoop()
	return e, nil
}

func (e *EdgeEndpoint) Addr() net.Addr {
	if e == nil || e.conn == nil {
		return nil
	}
	return e.conn.LocalAddr()
}

func (e *EdgeEndpoint) readLoop() {
	defer e.readerWG.Done()
	for {
		select {
		case <-e.closed:
			return
		default:
		}
		base := packetPool.Get().([]byte)
		n, addr, err := e.conn.ReadFromUDP(base)
		if err != nil {
			packetPool.Put(base)
			return
		}
		job := udpDatagramJob{data: base[:n], baseBuffer: base, remoteAddr: addr, realAddr: addr, receivedAt: time.Now()}
		queue := e.queues[udpDatagramShard(job.data, addr, len(e.queues))]
		select {
		case queue <- job:
		default:
			packetPool.Put(base)
		}
	}
}

func (e *EdgeEndpoint) worker(queue <-chan udpDatagramJob) {
	defer e.workerWG.Done()
	for job := range queue {
		func() {
			defer func() { _ = recover() }()
			e.handler(job.data, job.remoteAddr)
		}()
		packetPool.Put(job.baseBuffer)
	}
}

func (e *EdgeEndpoint) SendTo(data []byte, addr *net.UDPAddr) error {
	if e == nil || e.conn == nil || addr == nil {
		return errors.New("edge UDP endpoint is not ready")
	}
	_, err := e.conn.WriteToUDP(data, addr)
	return err
}

func (e *EdgeEndpoint) Fanout(data []byte, targets []EdgeFanoutTarget, sourceID int, sourceUser string, sourceSSID byte, onComplete func(EdgeFanoutResult)) bool {
	if e == nil || len(data) == 0 || len(targets) == 0 {
		return false
	}
	entries := make([]domainReceiverEntry, 0, len(targets))
	seen := make(map[netip.AddrPort]struct{}, len(targets))
	for _, target := range targets {
		addr, ok := udpAddrPort(target.Addr)
		if !ok {
			continue
		}
		if _, exists := seen[addr]; exists {
			continue
		}
		seen[addr] = struct{}{}
		entries = append(entries, domainReceiverEntry{addr: addr, deviceID: target.DeviceID, username: target.Username, ssid: target.SSID})
	}
	if len(entries) == 0 || e.sender == nil || len(e.sender.writers) == 0 {
		return false
	}
	partitions := make([][]domainReceiverEntry, len(e.sender.writers))
	for i := range entries {
		index := addrPortShard(entries[i].addr, len(partitions))
		partitions[index] = append(partitions[index], entries[i])
	}
	return e.sender.enqueue(fanoutFrameJob{
		data: append([]byte(nil), data...), partitions: partitions,
		sourceID: sourceID, sourceUser: sourceUser, sourceSSID: sourceSSID,
		enqueuedAt: time.Now(), validateGen: false,
		onComplete: func(result fanoutWriteResult) {
			if onComplete != nil {
				onComplete(EdgeFanoutResult{Attempted: result.attempted, Sent: result.sent, Dropped: result.dropped, Errors: result.errors})
			}
		},
	})
}

func (e *EdgeEndpoint) Close() error {
	if e == nil {
		return nil
	}
	var err error
	e.closeOnce.Do(func() {
		close(e.closed)
		if e.conn != nil {
			// Wake the single reader before closing duplicated Windows socket
			// views. A direct Close can wait for an outstanding IOCP read.
			wakeAddr := cloneEndpointAddr(e.conn.LocalAddr())
			if wakeAddr != nil {
				if wake, wakeErr := net.ListenUDP("udp", nil); wakeErr == nil {
					_, _ = wake.WriteToUDP([]byte{0}, wakeAddr)
					_ = wake.Close()
				}
			}
		}
		e.readerWG.Wait()
		for _, queue := range e.queues {
			close(queue)
		}
		e.workerWG.Wait()
		if e.sender != nil {
			e.sender.stop()
		}
		if e.conn != nil {
			err = e.conn.Close()
		}
	})
	return err
}

func cloneEndpointAddr(addr net.Addr) *net.UDPAddr {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok || udpAddr == nil {
		return nil
	}
	copyAddr := *udpAddr
	copyAddr.IP = append(net.IP(nil), udpAddr.IP...)
	if copyAddr.IP == nil || copyAddr.IP.IsUnspecified() {
		if udpAddr.IP != nil && udpAddr.IP.To4() == nil {
			copyAddr.IP = net.IPv6loopback
		} else {
			copyAddr.IP = net.IPv4(127, 0, 0, 1)
		}
	}
	return &copyAddr
}
