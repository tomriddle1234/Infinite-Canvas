package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"infinite-canvas/app-go/internal/config"
	"infinite-canvas/app-go/internal/handler"
	"infinite-canvas/app-go/internal/realtime"
	"infinite-canvas/app-go/internal/store"
)

type App struct {
	cfg    *config.Config
	engine *gin.Engine
}

func New(cfg *config.Config) (*App, error) {
	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(corsMiddleware())

	stores := store.New(cfg)
	realtimeManager := realtime.NewManager()
	handlers := handler.New(cfg, stores, realtimeManager)

	app := &App{cfg: cfg, engine: engine}
	app.registerStatic()
	app.registerWebSocket(realtimeManager)
	app.registerRoutes(handlers)
	return app, nil
}

func (a *App) Run() error {
	return a.engine.Run(a.cfg.Addr())
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "http://127.0.0.1:3000" || origin == "http://localhost:3000" ||
			origin == "http://127.0.0.1:8080" || origin == "http://localhost:8080" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization,X-User-Id")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
