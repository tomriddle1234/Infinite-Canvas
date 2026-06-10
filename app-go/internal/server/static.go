package server

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"infinite-canvas/app-go/web"
)

func (a *App) registerStatic() {
	staticFS, err := fs.Sub(web.StaticFiles, "static")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(staticFS))
	a.engine.GET("/", func(c *gin.Context) {
		serveEmbeddedFile(c, staticFS, "index.html")
	})
	a.engine.GET("/static/*filepath", func(c *gin.Context) {
		c.Request.URL.Path = strings.TrimPrefix(c.Request.URL.Path, "/static")
		fileServer.ServeHTTP(c.Writer, c.Request)
	})

	a.engine.StaticFS("/output", gin.Dir(a.cfg.OutputDir, false))
	a.engine.StaticFS("/assets", gin.Dir(a.cfg.AssetsDir, false))
}

func serveEmbeddedFile(c *gin.Context, filesystem fs.FS, name string) {
	cleaned := filepath.ToSlash(filepath.Clean(name))
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid path"})
		return
	}
	data, err := fs.ReadFile(filesystem, cleaned)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "file not found"})
		return
	}
	c.Data(http.StatusOK, contentType(cleaned), data)
}

func contentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".woff2":
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}
