package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"infinite-canvas/app-go/internal/realtime"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (a *App) registerWebSocket(manager *realtime.Manager) {
	a.engine.GET("/ws/stats", func(c *gin.Context) {
		conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		clientID := c.Query("client_id")
		manager.Add(conn, clientID)
		defer manager.Remove(conn)

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if string(message) == "ping" {
				manager.Send(conn, gin.H{"type": "pong"})
			}
		}
	})
}
