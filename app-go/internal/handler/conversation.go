package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"infinite-canvas/app-go/internal/store"
)

func (h *Handler) ListConversations(c *gin.Context) {
	userID := store.SafeUserID(c.GetHeader("X-User-Id"), c.ClientIP())
	c.JSON(http.StatusOK, gin.H{
		"user_id":       userID,
		"conversations": h.store.ListConversations(userID),
	})
}

type ConversationCreateRequest struct {
	Title string `json:"title"`
}

func (h *Handler) CreateConversation(c *gin.Context) {
	var payload ConversationCreateRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "请求参数不正确"})
		return
	}
	userID := store.SafeUserID(c.GetHeader("X-User-Id"), c.ClientIP())
	conversation, err := h.store.NewConversation(userID, payload.Title)
	if err != nil {
		writeStoreError(c, err, "对话不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversation": conversation})
}

func (h *Handler) GetConversation(c *gin.Context) {
	userID := store.SafeUserID(c.GetHeader("X-User-Id"), c.ClientIP())
	conversation, err := h.store.LoadConversation(userID, c.Param("id"))
	if err != nil {
		writeStoreError(c, err, "对话不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversation": conversation})
}

func (h *Handler) DeleteConversation(c *gin.Context) {
	userID := store.SafeUserID(c.GetHeader("X-User-Id"), c.ClientIP())
	if err := h.store.DeleteConversation(userID, c.Param("id")); err != nil {
		writeStoreError(c, err, "对话不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
