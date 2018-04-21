package rivulet

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

const (
	HeaderSize     = 4
	MaxMessageSize = 65531
)

var HUP = errors.New("HUP")
var Interrupted = errors.New("interrupt")

type Client struct {
	network string
	address string
	mtx     sync.Mutex
	rCond   *sync.Cond
	sCond   *sync.Cond
	hasRecv bool
	hasSend bool
	conn    *Connection
	// This mutex is for coordinating control logic so we establish at most
	// connection at a time.  Always acquire ctrlMtx before mtx.
	ctrlMtx sync.Mutex
}

func Connect(network, address string) *Client {
	c := &Client{
		network: network,
		address: address,
	}
	c.rCond = sync.NewCond(&c.mtx)
	c.sCond = sync.NewCond(&c.mtx)
	return c
}

func (c *Client) Recv() (string, error) {
	c.mtx.Lock()
	for c.hasRecv {
		c.rCond.Wait()
	}
	c.hasRecv = true
	conn := c.conn
	c.mtx.Unlock()
	defer func() {
		c.mtx.Lock()
		c.hasRecv = false
		c.mtx.Unlock()
		c.rCond.Signal()
	}()
	var err error
	if conn == nil {
		conn, err = c.connect()
		if err != nil {
			return "", err
		}
	}
	s, err := conn.Recv()
	if err != nil {
		return "", c.maybeHandleError(conn, err)
	}
	return s, nil
}

func (c *Client) Send(format string, args ...interface{}) error {
	c.mtx.Lock()
	for c.hasSend {
		c.sCond.Wait()
	}
	c.hasSend = true
	conn := c.conn
	c.mtx.Unlock()
	defer func() {
		c.mtx.Lock()
		c.hasSend = false
		c.mtx.Unlock()
		c.sCond.Signal()
	}()
	var err error
	if conn == nil {
		conn, err = c.connect()
		if err != nil {
			return err
		}
	}
	return c.maybeHandleError(conn, conn.Send(format, args...))
}

func (c *Client) HangUp() error {
	c.mtx.Lock()
	for c.hasSend {
		c.sCond.Wait()
	}
	c.hasSend = true
	conn := c.conn
	c.mtx.Unlock()
	defer func() {
		c.mtx.Lock()
		c.hasSend = false
		c.mtx.Unlock()
		c.sCond.Signal()
	}()
	if conn != nil {
		return conn.HangUp()
	} else {
		return nil
	}
}

func (c *Client) Reset() error {
	c.mtx.Lock()
	conn := c.conn
	c.conn = nil
	c.mtx.Unlock()
	if conn != nil {
		return conn.Close()
	} else {
		return nil
	}
}

func (c *Client) connect() (*Connection, error) {
	c.ctrlMtx.Lock()
	defer c.ctrlMtx.Unlock()
	c.mtx.Lock()
	conn := c.conn
	c.mtx.Unlock()
	if conn != nil {
		return conn, nil
	}
	raw, err := net.Dial(c.network, c.address)
	if err != nil {
		return nil, err
	}
	conn = &Connection{
		conn: raw,
	}
	c.mtx.Lock()
	c.conn = conn
	c.mtx.Unlock()
	return conn, nil
}

func (c *Client) maybeHandleError(conn *Connection, err error) error {
	if err == nil {
		return nil
	}
	c.mtx.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.mtx.Unlock()
	if conn != nil {
		// Intentionally drop this error as we are already handling a
		// potentially more informative error that's probably going to be the
		// root cause of any error this routine encounters.
		conn.Close()
	}
	return err
}

type Server struct {
	net.Listener
}

func NewServer(address string) (*Server, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	return &Server{listener}, nil
}

func (s *Server) Accept() (*Connection, error) {
	conn, err := s.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &Connection{
		conn: conn,
	}, nil
}

type Connection struct {
	conn net.Conn
}

func (c *Connection) Recv() (string, error) {
	header := [HeaderSize]byte{}
	err := c.read(header[:])
	if err != nil {
		return "", err
	}
	length, err := c.parseHeader(header)
	if err != nil {
		return "", err
	}
	msg := make([]byte, length-HeaderSize)
	err = c.read(msg)
	if err != nil {
		return "", err
	}
	return string(msg), nil
}

func (c *Connection) Send(format string, args ...interface{}) error {
	var s string
	if len(args) > 0 {
		s = fmt.Sprintf(format, args...)
	} else {
		s = format
	}
	if len(s) == 0 || s[len(s)-1] != '\n' {
		s += "\n"
	}
	msg := []byte(s)
	sz := len(msg)
	if sz > MaxMessageSize {
		return c.errorf("input", "message too long (%d > %d)", sz, MaxMessageSize)
	}
	header := lengthToHeader(sz + HeaderSize)
	err := c.write(header[:])
	if err != nil {
		return err
	}
	return c.write(msg)
}

func (c *Connection) HangUp() error {
	header := lengthToHeader(0)
	return c.write(header[:])
}

func (c *Connection) Close() error {
	return c.conn.Close()
}

func (c *Connection) parseHeader(header [HeaderSize]byte) (int, error) {
	raw := make([]byte, 2)
	n, err := hex.Decode(raw, header[:])
	if err != nil {
		return 0, c.wrap("parse", err)
	}
	if n != 2 {
		return 0, c.wrap("parse", errors.New("malformed header"))
	}
	length := int(binary.BigEndian.Uint16(raw))
	if length == 0 {
		return 0, HUP
	} else if length < HeaderSize {
		return 0, c.wrap("parse", errors.New("malformed header"))
	}
	return length, nil
}

func (c *Connection) read(buf []byte) error {
	n, err := io.ReadFull(c.conn, buf)
	if err == nil && n != len(buf) {
		err = c.errorf("read", "short recv (%d < %d)", n, len(buf))
	}
	return c.wrap("read", err)
}

func (c *Connection) write(buf []byte) error {
	n, err := c.conn.Write(buf)
	if err == nil && n != len(buf) {
		err = c.errorf("write", "short write (%d < %d)", n, len(buf))
	}
	return c.wrap("write", err)
}

func (c *Connection) errorf(op, format string, args ...interface{}) error {
	return c.wrap(op, fmt.Errorf(format, args...))
}

func (c *Connection) wrap(op string, err error) error {
	// TODO(rescrv): wrap this error
	return err
}

func lengthToHeader(sz int) [HeaderSize]byte {
	var raw [HeaderSize / 2]byte
	var header [HeaderSize]byte
	binary.BigEndian.PutUint16(raw[:], uint16(sz))
	hex.Encode(header[:], raw[:])
	return header
}
