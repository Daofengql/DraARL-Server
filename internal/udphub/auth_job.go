package udphub

import (
	"context"
	"log"
	"net"
	"runtime/debug"
	"sync"
	"time"

	"draarl/internal/config"
	"draarl/internal/models"
	"draarl/internal/protocol"
)

// deviceAuthJob owns a packet copy because ingress buffers return to the pool
// before expensive credential verification completes.
type deviceAuthJob struct {
	ctx                   context.Context
	packet                protocol.DraARLv1Packet
	data                  []byte
	dev                   *models.Device
	conn                  *net.UDPConn
	remoteAddr            *net.UDPAddr
	realAddr              *net.UDPAddr
	incomingMAC           string
	needsCenterActivation bool
}

const (
	defaultDeviceAuthWorkers   = 4
	defaultDeviceAuthQueueSize = 512
	maxDeviceAuthWorkers       = 16
	maxDeviceAuthQueueSize     = 2048
	deviceAuthBackendTimeout   = 5 * time.Second
)

type deviceAuthAdmission uint8

type deviceAuthProcessor func(*deviceAuthJob)

const (
	deviceAuthRejected deviceAuthAdmission = iota
	deviceAuthAccepted
	deviceAuthCoalesced
)

var (
	deviceAuthQueues   []chan *deviceAuthJob
	deviceAuthPending  map[string]struct{}
	deviceAuthWg       sync.WaitGroup
	deviceAuthMu       sync.Mutex
	deviceAuthStarted  bool
	deviceAuthStopping bool
	deviceAuthCtx      context.Context
	deviceAuthCancel   context.CancelFunc
)

func deviceAuthRuntimeSettings() (workers, queueSize int) {
	workers, queueSize = defaultDeviceAuthWorkers, defaultDeviceAuthQueueSize
	if cfg := config.TryGet(); cfg != nil {
		if cfg.UDP.DeviceAuthWorkers > 0 {
			workers = cfg.UDP.DeviceAuthWorkers
		}
		if cfg.UDP.DeviceAuthQueueSize > 0 {
			queueSize = cfg.UDP.DeviceAuthQueueSize
		}
	}
	if workers < 1 {
		workers = 1
	} else if workers > maxDeviceAuthWorkers {
		workers = maxDeviceAuthWorkers
	}
	if queueSize < 1 {
		queueSize = 1
	} else if queueSize > maxDeviceAuthQueueSize {
		queueSize = maxDeviceAuthQueueSize
	}
	return workers, queueSize
}

// startDeviceAuthWorkers bounds costly authentication independently from UDP
// ingress. A worker queue is deliberately generous so a shared NAT/FRP restart
// is admitted as a burst rather than rejected by a source-IP quota.
func startDeviceAuthWorkers() {
	startDeviceAuthWorkersWith(processDeviceAuthJob)
}

func startDeviceAuthWorkersWith(process deviceAuthProcessor) {
	deviceAuthMu.Lock()
	defer deviceAuthMu.Unlock()
	if deviceAuthStarted || deviceAuthStopping {
		return
	}
	if process == nil {
		process = processDeviceAuthJob
	}
	workers, queueSize := deviceAuthRuntimeSettings()
	ctx, cancel := context.WithCancel(context.Background())
	deviceAuthQueues = make([]chan *deviceAuthJob, workers)
	deviceAuthPending = make(map[string]struct{})
	deviceAuthCtx = ctx
	deviceAuthCancel = cancel
	for i := 0; i < workers; i++ {
		deviceAuthQueues[i] = make(chan *deviceAuthJob, queueSize)
		deviceAuthWg.Add(1)
		go deviceAuthWorkerLoop(deviceAuthQueues[i], process)
	}
	deviceAuthStarted = true
	log.Printf("[AUTH] device authentication pool started: workers=%d queue_per_worker=%d", workers, queueSize)
}

// stopDeviceAuthWorkers closes admission before closing queues, so no sender
// can race a close while shutdown waits for ingress workers to exit. The
// stopping flag also prevents a new generation from starting until all old
// workers have released their identity reservations.
func stopDeviceAuthWorkers() {
	deviceAuthMu.Lock()
	if !deviceAuthStarted || deviceAuthStopping {
		deviceAuthMu.Unlock()
		return
	}
	deviceAuthStopping = true
	queues := deviceAuthQueues
	cancel := deviceAuthCancel
	deviceAuthMu.Unlock()
	if cancel != nil {
		cancel()
	}

	for _, queue := range queues {
		close(queue)
	}
	deviceAuthWg.Wait()
	deviceAuthMu.Lock()
	deviceAuthQueues = nil
	deviceAuthPending = nil
	deviceAuthCtx = nil
	deviceAuthCancel = nil
	deviceAuthStarted = false
	deviceAuthStopping = false
	deviceAuthMu.Unlock()
	log.Println("[AUTH] device authentication pool stopped")
}

func deviceAuthIdentity(job *deviceAuthJob) string {
	if job == nil {
		return ""
	}
	if job.packet.Username != "" {
		return protocol.GetUsernameSSID(job.packet.Username, job.packet.SSID)
	}
	if job.remoteAddr != nil {
		return "addr:" + job.remoteAddr.String()
	}
	return ""
}

// enqueueDeviceAuth admits one pending verification per device identity.
// Repeated heartbeats coalesce behind the first job; callers must not send an
// auth_busy reply for a coalesced job because the admitted job will respond.
func enqueueDeviceAuth(job *deviceAuthJob) deviceAuthAdmission {
	if job == nil || len(job.data) == 0 {
		return deviceAuthRejected
	}
	identity := deviceAuthIdentity(job)
	deviceAuthMu.Lock()
	defer deviceAuthMu.Unlock()
	if !deviceAuthStarted || deviceAuthStopping || len(deviceAuthQueues) == 0 {
		return deviceAuthRejected
	}
	if identity != "" {
		if _, exists := deviceAuthPending[identity]; exists {
			return deviceAuthCoalesced
		}
	}
	if job.ctx == nil {
		job.ctx = deviceAuthCtx
	}
	index := udpDatagramShard(job.data, job.remoteAddr, len(deviceAuthQueues))
	select {
	case deviceAuthQueues[index] <- job:
		if identity != "" {
			deviceAuthPending[identity] = struct{}{}
		}
		return deviceAuthAccepted
	default:
		return deviceAuthRejected
	}
}

func releaseDeviceAuthIdentity(job *deviceAuthJob) {
	identity := deviceAuthIdentity(job)
	if identity == "" {
		return
	}
	deviceAuthMu.Lock()
	delete(deviceAuthPending, identity)
	deviceAuthMu.Unlock()
}

func deviceAuthWorkerLoop(queue <-chan *deviceAuthJob, process deviceAuthProcessor) {
	defer deviceAuthWg.Done()
	deviceAuthWorkerLoopWith(queue, process)
}

func deviceAuthWorkerLoopWith(queue <-chan *deviceAuthJob, process deviceAuthProcessor) {
	if process == nil {
		process = processDeviceAuthJob
	}
	for job := range queue {
		func() {
			defer releaseDeviceAuthIdentity(job)
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf("[AUTH] recovered panic while authenticating %s: %v\n%s",
						deviceAuthIdentity(job), recovered, debug.Stack())
				}
			}()
			process(job)
		}()
	}
}

func newDeviceAuthJob(
	packet *protocol.DraARLv1Packet,
	data []byte,
	dev *models.Device,
	conn *net.UDPConn,
	realAddr *net.UDPAddr,
	incomingMAC string,
	needsCenterActivation bool,
) *deviceAuthJob {
	if packet == nil || len(data) < protocol.DraARLv1HeaderSize {
		return nil
	}
	dataCopy := append([]byte(nil), data...)
	packetCopy := *packet
	packetCopy.DATA = dataCopy[protocol.DraARLv1HeaderSize:]
	packetCopy.Reserved = dataCopy[protocol.DraARLv1ReservedOffset:protocol.DraARLv1HeaderSize]
	return &deviceAuthJob{
		packet: packetCopy, data: dataCopy, dev: dev, conn: conn,
		remoteAddr: packet.UDPAddr, realAddr: realAddr, incomingMAC: incomingMAC,
		needsCenterActivation: needsCenterActivation,
	}
}

func queueDeviceAuthentication(
	packet *protocol.DraARLv1Packet,
	data []byte,
	dev *models.Device,
	conn *net.UDPConn,
	realAddr *net.UDPAddr,
	incomingMAC string,
	needsCenterActivation bool,
) deviceAuthAdmission {
	return enqueueDeviceAuth(newDeviceAuthJob(packet, data, dev, conn, realAddr, incomingMAC, needsCenterActivation))
}

func processDeviceAuthJob(job *deviceAuthJob) {
	if job == nil {
		return
	}
	packet := &job.packet
	if job.realAddr == nil || job.realAddr.IP == nil {
		sendHeartbeatReject(job.conn, packet, protocol.HeartbeatStatusAuthFailed, "invalid_source")
		return
	}
	baseCtx := job.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, deviceAuthBackendTimeout)
	defer cancel()
	authResult := AuthenticateDeviceContext(ctx, job.realAddr.IP.String(), packet.Username, packet.DevicePassword)
	if !authResult.Success {
		log.Printf("[AUTH] device authentication failed: %s, error: %s",
			protocol.GetUsernameSSID(packet.Username, packet.SSID), authResult.Error)
		sendHeartbeatReject(job.conn, packet, protocol.HeartbeatStatusAuthFailed, authResult.Error)
		return
	}
	if job.ctx != nil {
		if err := job.ctx.Err(); err != nil {
			sendHeartbeatReject(job.conn, packet, protocol.HeartbeatStatusAuthFailed, authContextError(job.ctx))
			return
		}
	}
	if authResult.User == nil {
		sendHeartbeatReject(job.conn, packet, protocol.HeartbeatStatusAuthFailed, "user_not_found")
		return
	}
	if job.dev == nil {
		registerAuthenticatedDraARLDevice(packet, job.realAddr, job.conn, authResult, job.incomingMAC)
		return
	}
	conflictAddr := normalDeviceConflictAddr(job.dev, packet.UDPAddr, job.realAddr)
	if shouldRejectNormalDeviceConflictForModel(job.dev, conflictAddr, job.incomingMAC, packet.DevModel) {
		state := job.dev.RuntimeSnapshot()
		log.Printf("[AUTH] device conflict rejected: owner_id=%d ssid=%d existing_addr=%v new_addr=%v",
			state.OwnerID, state.SSID, state.UDPAddr, packet.UDPAddr)
		sendHeartbeatReject(job.conn, packet, protocol.HeartbeatStatusDeviceConflictOnline, "device_conflict_online")
		return
	}
	job.dev.UpdateRuntime(func(current *models.Device) {
		current.CallSign = authResult.CallSign
		current.Username = authResult.User.Name
		current.Nickname = authResult.User.NickName
		current.UDPAddr = packet.UDPAddr
		current.RealUDPAddr = job.realAddr
		if job.realAddr != nil && job.realAddr.IP != nil {
			current.LastOnlineIP = job.realAddr.IP.String()
		}
		if job.incomingMAC != "" {
			current.MAC = job.incomingMAC
		}
	})
	state := job.dev.RuntimeSnapshot()
	log.Printf("[AUTH] device re-authenticated: %s-%d (%s) from %v",
		protocol.GetUsernameSSID(packet.Username, packet.SSID), state.SSID, state.CallSign, packet.UDPAddr)
	if job.needsCenterActivation {
		if err := activateAndPersistCenterDeviceContext(ctx, job.dev); err != nil {
			log.Printf("[INTERCONNECT] activate centre device %d failed: %v", job.dev.ID, err)
			sendHeartbeatReject(job.conn, packet, protocol.HeartbeatStatusAuthFailed, "center_session_activation_failed")
			return
		}
	}
	continueDraARLPacket(packet, job.data, job.dev, job.conn, job.realAddr, false)
}
