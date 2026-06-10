package store

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var safeConversationIDPattern = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
var safeUserIDPattern = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)

func SafeUserID(headerValue, clientIP string) string {
	candidate := strings.TrimSpace(headerValue)
	if candidate == "" {
		if clientIP != "" {
			candidate = "ip-" + clientIP
		}
	}
	if candidate == "" {
		candidate = "anonymous"
	}
	candidate = safeUserIDPattern.ReplaceAllString(candidate, "-")
	if len(candidate) > 80 {
		candidate = candidate[:80]
	}
	candidate = strings.Trim(candidate, ".-")
	if candidate == "" {
		return "anonymous"
	}
	return candidate
}

func (s *Store) userDir(userID string) string {
	path := filepath.Join(s.cfg.ConversationDir, userID)
	_ = os.MkdirAll(path, 0o755)
	return path
}

func (s *Store) ConversationPath(userID, conversationID string) (string, bool) {
	cleaned := safeConversationIDPattern.ReplaceAllString(conversationID, "")
	if cleaned == "" {
		return "", false
	}
	return filepath.Join(s.userDir(userID), cleaned+".json"), true
}

func (s *Store) LoadConversation(userID, conversationID string) (map[string]any, error) {
	path, ok := s.ConversationPath(userID, conversationID)
	if !ok {
		return nil, ErrBadID
	}
	conversation, err := readJSONMap(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return conversation, nil
}

func (s *Store) NewConversation(userID, title string) (map[string]any, error) {
	now := NowMS()
	conversation := map[string]any{
		"id":         NewHexID(),
		"title":      trimStringRunes(title, "新对话", 80),
		"created_at": now,
		"updated_at": now,
		"messages":   []any{},
	}
	if err := s.SaveConversation(userID, conversation); err != nil {
		return nil, err
	}
	return conversation, nil
}

func (s *Store) SaveConversation(userID string, conversation map[string]any) error {
	path, ok := s.ConversationPath(userID, stringValue(conversation["id"], ""))
	if !ok {
		return ErrBadID
	}
	s.conversationMu.Lock()
	defer s.conversationMu.Unlock()
	return writeJSON(path, conversation)
}

func (s *Store) DeleteConversation(userID, conversationID string) error {
	path, ok := s.ConversationPath(userID, conversationID)
	if !ok {
		return ErrBadID
	}
	s.conversationMu.Lock()
	defer s.conversationMu.Unlock()
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) ListConversations(userID string) []map[string]any {
	entries, err := os.ReadDir(s.userDir(userID))
	if err != nil {
		return []map[string]any{}
	}
	records := make([]map[string]any, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := readJSONMap(filepath.Join(s.userDir(userID), entry.Name()))
		if err != nil {
			continue
		}
		records = append(records, map[string]any{
			"id":           data["id"],
			"title":        stringValue(data["title"], "新对话"),
			"created_at":   valueOr(data["created_at"], 0),
			"updated_at":   valueOr(data["updated_at"], 0),
			"last_message": lastMessageContent(data["messages"]),
		})
	}
	sort.Slice(records, func(i, j int) bool {
		return number(records[i]["updated_at"]) > number(records[j]["updated_at"])
	})
	return records
}

func lastMessageContent(value any) string {
	messages, ok := value.([]any)
	if !ok {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		if stringValue(msg["role"], "") == "system" {
			continue
		}
		return stringValue(msg["content"], "")
	}
	return ""
}
