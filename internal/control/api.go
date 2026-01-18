// Package control provides the daemon control plane API.
package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
)

// Server handles incoming connections on the Unix socket.
type Server struct {
	socketPath string
	listener   net.Listener
	handlers   map[string]HandlerFunc
	mu         sync.RWMutex
	clients    map[*clientConn]struct{}
	done       chan struct{}
}

type clientConn struct {
	conn    net.Conn
	writeMu sync.Mutex
}

func (c *clientConn) write(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.conn.Write(data)
	return err
}

// HandlerFunc is the signature for API method handlers.
type HandlerFunc func(params json.RawMessage) (any, error)

// Request represents an incoming API request.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	ID     string          `json:"id,omitempty"`
}

// Response represents an outgoing API response.
type Response struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
	ID    string `json:"id,omitempty"`
}

// Event represents a pushed event to clients.
type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// NewServer creates a new control server.
func NewServer(socketPath string) *Server {
	return &Server{
		socketPath: socketPath,
		handlers:   make(map[string]HandlerFunc),
		clients:    make(map[*clientConn]struct{}),
		done:       make(chan struct{}),
	}
}

// Handle registers a handler for a method.
func (s *Server) Handle(method string, handler HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = handler
}

// Start begins listening for connections.
func (s *Server) Start() error {
	if conn, err := net.Dial("unix", s.socketPath); err == nil {
		conn.Close()
		return fmt.Errorf("daemon already running (socket %s)", s.socketPath)
	}

	// Remove existing socket
	os.Remove(s.socketPath)

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}
	s.listener = listener

	// Set permissions
	os.Chmod(s.socketPath, 0700)

	go s.acceptLoop()
	return nil
}

// Stop closes the server.
func (s *Server) Stop() error {
	close(s.done)

	s.mu.Lock()
	for client := range s.clients {
		client.conn.Close()
	}
	s.mu.Unlock()

	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(s.socketPath)
	return nil
}

// Broadcast sends an event to all connected clients.
func (s *Server) Broadcast(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	data = append(data, '\n')

	s.mu.RLock()
	defer s.mu.RUnlock()

	for client := range s.clients {
		client.write(data)
	}
}

func (s *Server) acceptLoop() {
	for {
		select {
		case <-s.done:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}

		s.mu.Lock()
		client := &clientConn{conn: conn}
		s.clients[client] = struct{}{}
		s.mu.Unlock()

		go s.handleConnection(client)
	}
}

func (s *Server) handleConnection(client *clientConn) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, client)
		s.mu.Unlock()
		client.conn.Close()
	}()

	scanner := bufio.NewScanner(client.conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			s.sendError(client, "", "invalid request: "+err.Error())
			continue
		}

		s.mu.RLock()
		handler, ok := s.handlers[req.Method]
		s.mu.RUnlock()

		if !ok {
			s.sendError(client, req.ID, "unknown method: "+req.Method)
			continue
		}

		data, err := handler(req.Params)
		if err != nil {
			s.sendError(client, req.ID, err.Error())
			continue
		}

		s.sendResponse(client, req.ID, data)
	}
}

func (s *Server) sendResponse(client *clientConn, id string, data any) {
	resp := Response{Data: data, ID: id}
	encoded, _ := json.Marshal(resp)
	client.write(append(encoded, '\n'))
}

func (s *Server) sendError(client *clientConn, id, errMsg string) {
	resp := Response{Error: errMsg, ID: id}
	encoded, _ := json.Marshal(resp)
	client.write(append(encoded, '\n'))
}
