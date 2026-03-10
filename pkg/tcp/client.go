package tcp

import (
	"bufio"
	"log"
	"net"
	"sync"
	"time"
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
}

// NewClient 创建新的TCP客户端
func NewClient(host, port string, onMessage func(message []byte)) *Client {
	return &Client{
		host:      host,
		port:      port,
		onMessage: onMessage,
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
	c.mu.Lock()
	defer c.mu.Unlock()

	for {
		conn, err := net.Dial("tcp", net.JoinHostPort(c.host, c.port))
		if err != nil {
			log.Printf("无法连接到TCP服务器 %s:%s: %v", c.host, c.port, err)
			time.Sleep(5 * time.Second)
			continue
		}

		c.conn = conn
		c.connected = true
		log.Printf("已连接到TCP服务器 %s:%s", c.host, c.port)

		// 启动读取消息的 goroutine
		go c.readMessages()

		return nil
	}
}

// Send 发送消息
func (c *Client) Send(message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.conn == nil {
		return net.ErrWriteToConnected
	}

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
func (c *Client) readMessages() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("TCP客户端读取消息panic: %v", r)
		}
	}()

	reader := bufio.NewReader(c.conn)
	for {
		message, err := reader.ReadBytes('\n')
		if err != nil {
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()

			if c.onError != nil {
				c.onError(err)
			}
			log.Printf("TCP客户端读取消息错误: %v", err)
			return
		}

		if c.onMessage != nil {
			c.onMessage(message)
		}
	}
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
