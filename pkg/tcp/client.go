package tcp

import (
	"bufio"
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

// TCP 客户端读写保护常量：
// 【修复】写超时避免死对端写缓冲打满永久阻塞；读超时避免静默对端永久阻塞；
// 单行上限防止无换行超长报文耗尽内存。
const (
	tcpWriteTimeout = 10 * time.Second
	tcpReadTimeout  = 60 * time.Second
	tcpMaxLineSize  = 1 << 20 // 1MB
)

// Client TCP客户端
type Client struct {
	host      string
	port      string
	conn      net.Conn
	onMessage func(message []byte)
	onError   func(error)
	mu        sync.Mutex
	connected bool
	stopChan  chan struct{}
}

// NewClient 创建新的TCP客户端
func NewClient(host, port string, onMessage func(message []byte)) *Client {
	return &Client{
		host:      host,
		port:      port,
		onMessage: onMessage,
		stopChan:  make(chan struct{}),
	}
}

// SetOnError 设置错误回调
func (c *Client) SetOnError(onError func(error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onError = onError
}

// Connect 连接到TCP服务器
func (c *Client) Connect() error {
	for {
		select {
		case <-c.stopChan:
			return net.ErrClosed
		default:
		}

		conn, err := c.dial()
		if err != nil {
			log.Printf("无法连接到TCP服务器 %s:%s: %v", c.host, c.port, err)
			select {
			case <-c.stopChan:
				return net.ErrClosed
			case <-time.After(5 * time.Second):
			}
			continue
		}

		select {
		case <-c.stopChan:
			_ = conn.Close()
			return net.ErrClosed
		default:
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			_ = tcpConn.SetKeepAlive(true)
			_ = tcpConn.SetKeepAlivePeriod(30 * time.Second)
		}
		c.mu.Lock()
		c.conn = conn
		c.connected = true
		c.mu.Unlock()
		log.Printf("已连接到TCP服务器 %s:%s", c.host, c.port)

		// 启动读取消息的 goroutine
		go c.readMessages(conn)

		return nil
	}
}

func (c *Client) dial() (net.Conn, error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		select {
		case <-c.stopChan:
			cancel()
		case <-done:
		}
	}()
	defer close(done)
	defer cancel()

	dialer := net.Dialer{Timeout: 10 * time.Second}
	return dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.host, c.port))
}

// Send 发送消息
func (c *Client) Send(message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.conn == nil {
		return net.ErrWriteToConnected
	}

	_ = c.conn.SetWriteDeadline(time.Now().Add(tcpWriteTimeout))
	_, err := c.conn.Write([]byte(message))
	if err != nil {
		c.close()
		return err
	}

	return nil
}

// SendBytes 发送字节数组
func (c *Client) SendBytes(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.conn == nil {
		return net.ErrWriteToConnected
	}

	_ = c.conn.SetWriteDeadline(time.Now().Add(tcpWriteTimeout))
	_, err := c.conn.Write(data)
	if err != nil {
		c.close()
		return err
	}

	return nil
}

// Close 关闭连接
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.stopChan:
	default:
		close(c.stopChan)
	}

	return c.close()
}

// close 内部关闭方法（不加锁）
func (c *Client) close() error {
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.connected = false
		return err
	}
	c.connected = false
	return nil
}

// IsConnected 检查是否已连接
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// readMessages 读取消息
func (c *Client) readMessages(conn net.Conn) {
	if conn == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("TCP客户端读取消息panic: %v", r)
		}
	}()

	reader := bufio.NewReader(conn)
	for {
		// 滚动读超时：静默对端不再永久阻塞
		_ = conn.SetReadDeadline(time.Now().Add(tcpReadTimeout))
		line := make([]byte, 0, 256)
		lineTooLong := false
		for {
			fragment, err := reader.ReadSlice('\n')
			line = append(line, fragment...)
			if len(line) > tcpMaxLineSize {
				lineTooLong = true
				break
			}
			if err == nil {
				break
			}
			if err != bufio.ErrBufferFull {
				c.handleReadError(conn, err)
				return
			}
		}
		if lineTooLong {
			c.handleReadError(conn, errors.New("tcp line exceeds size limit"))
			return
		}
		c.mu.Lock()
		onMessage := c.onMessage
		c.mu.Unlock()
		if onMessage != nil {
			onMessage(line)
		}
	}
}

func (c *Client) handleReadError(conn net.Conn, err error) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
		c.connected = false
	}
	onError := c.onError
	c.mu.Unlock()
	// Always close the reader's own connection. If a reconnect already
	// installed a different connection, the identity check above keeps it
	// untouched while this closes only the stale socket.
	_ = conn.Close()
	if onError != nil {
		onError(err)
	}
	log.Printf("TCP客户端读取消息错误: %v", err)
}

// GetRemoteAddr 获取远程地址
func (c *Client) GetRemoteAddr() net.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.RemoteAddr()
	}
	return nil
}
