package store

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"infinite-canvas/app-go/internal/config"
)

var safeIDPattern = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func (s *Store) CanvasPath(canvasID string) (string, bool) {
	cleaned := safeIDPattern.ReplaceAllString(canvasID, "")
	if cleaned == "" {
		return "", false
	}
	return filepath.Join(s.cfg.CanvasDir, cleaned+".json"), true
}

func (s *Store) LoadCanvas(canvasID string, includeDeleted bool) (map[string]any, error) {
	path, ok := s.CanvasPath(canvasID)
	if !ok {
		return nil, ErrBadID
	}
	canvas, err := readJSONMap(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !includeDeleted && truthy(canvas["deleted_at"]) {
		return nil, ErrCanvasDeleted
	}
	return canvas, nil
}

func (s *Store) NewCanvas(title, icon, kind string) (map[string]any, error) {
	now := NowMS()
	canvas := map[string]any{
		"id":          NewHexID(),
		"title":       trimStringRunes(title, "未命名画布", 80),
		"icon":        trimStringRunes(icon, "🧩", 32),
		"kind":        "classic",
		"created_at":  now,
		"updated_at":  now,
		"nodes":       []any{},
		"connections": []any{},
		"viewport":    map[string]any{"x": 0, "y": 0, "scale": 1},
		"logs":        []any{},
		"settings":    map[string]any{},
	}
	if kind != "" {
		canvas["kind"] = "classic"
	}
	if err := s.SaveCanvas(canvas); err != nil {
		return nil, err
	}
	return canvas, nil
}

func (s *Store) SaveCanvas(canvas map[string]any) error {
	id := stringValue(canvas["id"], "")
	path, ok := s.CanvasPath(id)
	if !ok {
		return ErrBadID
	}
	canvas["updated_at"] = NowMS()
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	return writeJSON(path, canvas)
}

func (s *Store) SoftDeleteCanvas(canvasID string) error {
	canvas, err := s.LoadCanvas(canvasID, true)
	if err != nil {
		return err
	}
	if !truthy(canvas["deleted_at"]) {
		canvas["deleted_at"] = NowMS()
		return s.SaveCanvas(canvas)
	}
	return nil
}

func (s *Store) RestoreCanvas(canvasID string) (map[string]any, error) {
	canvas, err := s.LoadCanvas(canvasID, true)
	if err != nil {
		return nil, err
	}
	if truthy(canvas["deleted_at"]) {
		delete(canvas, "deleted_at")
		if err := s.SaveCanvas(canvas); err != nil {
			return nil, err
		}
	}
	return canvas, nil
}

func (s *Store) PurgeCanvas(canvasID string) error {
	path, ok := s.CanvasPath(canvasID)
	if !ok {
		return ErrBadID
	}
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) ListCanvases(includeDeleted bool) []map[string]any {
	s.cleanupExpiredCanvasTrash()

	entries, err := os.ReadDir(s.cfg.CanvasDir)
	if err != nil {
		return []map[string]any{}
	}
	records := make([]map[string]any, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := readJSONMap(filepath.Join(s.cfg.CanvasDir, entry.Name()))
		if err != nil {
			continue
		}
		deleted := truthy(data["deleted_at"])
		if includeDeleted != deleted {
			continue
		}
		records = append(records, canvasRecord(data))
	}

	key := "updated_at"
	if includeDeleted {
		key = "deleted_at"
	}
	sort.Slice(records, func(i, j int) bool {
		return number(records[i][key]) > number(records[j][key])
	})
	return records
}

func (s *Store) cleanupExpiredCanvasTrash() {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()

	entries, err := os.ReadDir(s.cfg.CanvasDir)
	if err != nil {
		return
	}
	cutoff := float64(time.Now().UnixMilli() - config.CanvasTrashRetentionMS)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.cfg.CanvasDir, entry.Name())
		data, err := readJSONMap(path)
		if err != nil {
			continue
		}
		deletedAt := number(data["deleted_at"])
		if deletedAt > 0 && deletedAt < cutoff {
			_ = os.Remove(path)
		}
	}
}

func canvasRecord(data map[string]any) map[string]any {
	return map[string]any{
		"id":         data["id"],
		"title":      stringValue(data["title"], "未命名画布"),
		"icon":       stringValue(data["icon"], "🧩"),
		"kind":       stringValue(data["kind"], "classic"),
		"created_at": valueOr(data["created_at"], 0),
		"updated_at": valueOr(data["updated_at"], 0),
		"deleted_at": valueOr(data["deleted_at"], 0),
		"node_count": lenArray(data["nodes"]),
	}
}
