package interconnect

import "sync"

type nodeStatusEvent struct {
	session   *NodeSession
	heartbeat *NodeHeartbeat
	online    bool
}

// NodeStatusDispatcher keeps periodic database/monitoring work out of the TLS
// read loop. Heartbeats are coalesced per NodeID, while low-frequency connect
// and disconnect events are applied synchronously to preserve lifecycle order.
type NodeStatusDispatcher struct {
	handler func(*NodeSession, *NodeHeartbeat, bool)

	mu      sync.Mutex
	pending map[string]nodeStatusEvent
	wake    chan struct{}
	closed  chan struct{}
	done    chan struct{}
	once    sync.Once
}

func NewNodeStatusDispatcher(handler func(*NodeSession, *NodeHeartbeat, bool)) *NodeStatusDispatcher {
	if handler == nil {
		return nil
	}
	d := &NodeStatusDispatcher{
		handler: handler,
		pending: make(map[string]nodeStatusEvent),
		wake:    make(chan struct{}, 1),
		closed:  make(chan struct{}),
		done:    make(chan struct{}),
	}
	go d.run()
	return d
}

func (d *NodeStatusDispatcher) Submit(session *NodeSession, heartbeat *NodeHeartbeat, online bool) {
	if d == nil || session == nil || session.NodeID == "" {
		return
	}
	if heartbeat == nil {
		d.mu.Lock()
		delete(d.pending, session.NodeID)
		d.mu.Unlock()
		d.handler(session, nil, online)
		return
	}
	event := nodeStatusEvent{session: session, online: online}
	copyHeartbeat := *heartbeat
	event.heartbeat = &copyHeartbeat
	d.mu.Lock()
	select {
	case <-d.closed:
		d.mu.Unlock()
		return
	default:
	}
	d.pending[session.NodeID] = event
	d.mu.Unlock()
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *NodeStatusDispatcher) run() {
	defer close(d.done)
	for {
		select {
		case <-d.wake:
			d.drain()
		case <-d.closed:
			d.drain()
			return
		}
	}
}

func (d *NodeStatusDispatcher) drain() {
	for {
		d.mu.Lock()
		if len(d.pending) == 0 {
			d.mu.Unlock()
			return
		}
		batch := d.pending
		d.pending = make(map[string]nodeStatusEvent)
		d.mu.Unlock()
		for _, event := range batch {
			d.handler(event.session, event.heartbeat, event.online)
		}
	}
}

func (d *NodeStatusDispatcher) Close() {
	if d == nil {
		return
	}
	d.once.Do(func() { close(d.closed) })
	<-d.done
}
