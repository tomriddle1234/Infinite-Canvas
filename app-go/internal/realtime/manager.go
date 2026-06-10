package realtime

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

type Manager struct {
	mu          sync.Mutex
	connections map[*websocket.Conn]string
}

func NewManager() *Manager {
	return &Manager{connections: map[*websocket.Conn]string{}}
}

func (m *Manager) Add(conn *websocket.Conn, clientID string) {
	m.mu.Lock()
	m.connections[conn] = clientID
	count := len(m.connections)
	m.mu.Unlock()
	m.broadcastJSON(map[string]any{"type": "stats", "online_count": count})
}

func (m *Manager) Remove(conn *websocket.Conn) {
	m.mu.Lock()
	delete(m.connections, conn)
	count := len(m.connections)
	m.mu.Unlock()
	_ = conn.Close()
	m.broadcastJSON(map[string]any{"type": "stats", "online_count": count})
}

func (m *Manager) Send(conn *websocket.Conn, payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.connections[conn]; !ok {
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		delete(m.connections, conn)
		_ = conn.Close()
	}
}

func (m *Manager) BroadcastCanvasUpdated(canvasID string, updatedAt any, clientID string) {
	m.broadcastJSON(map[string]any{
		"type":       "canvas_updated",
		"canvas_id":  canvasID,
		"updated_at": updatedAt,
		"client_id":  clientID,
	})
}

func (m *Manager) BroadcastNewImage(data map[string]any) {
	m.broadcastJSON(map[string]any{"type": "new_image", "data": data})
}

func (m *Manager) broadcastJSON(payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	m.mu.Lock()
	failed := make([]*websocket.Conn, 0)
	for conn := range m.connections {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			failed = append(failed, conn)
		}
	}
	for _, conn := range failed {
		delete(m.connections, conn)
		_ = conn.Close()
	}
	m.mu.Unlock()
}
