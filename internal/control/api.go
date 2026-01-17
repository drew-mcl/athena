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
	clients    map[net.Conn]struct{}
	done       chan struct{}
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
		clients:    make(map[net.Conn]struct{}),
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
	for conn := range s.clients {
		conn.Close()
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

	for conn := range s.clients {
		conn.Write(data)
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
		s.clients[conn] = struct{}{}
		s.mu.Unlock()

		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
		conn.Close()
	}()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			s.sendError(conn, "", "invalid request: "+err.Error())
			continue
		}

		s.mu.RLock()
		handler, ok := s.handlers[req.Method]
		s.mu.RUnlock()

		if !ok {
			s.sendError(conn, req.ID, "unknown method: "+req.Method)
			continue
		}

		data, err := handler(req.Params)
		if err != nil {
			s.sendError(conn, req.ID, err.Error())
			continue
		}

		s.sendResponse(conn, req.ID, data)
	}
}

func (s *Server) sendResponse(conn net.Conn, id string, data any) {
	resp := Response{Data: data, ID: id}
	encoded, _ := json.Marshal(resp)
	conn.Write(append(encoded, '\n'))
}

func (s *Server) sendError(conn net.Conn, id, errMsg string) {
	resp := Response{Error: errMsg, ID: id}
	encoded, _ := json.Marshal(resp)
	conn.Write(append(encoded, '\n'))
}
