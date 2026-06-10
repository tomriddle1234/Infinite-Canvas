package store

import (
	"path/filepath"
	"strings"
)

func (s *Store) seedanceTasksFile() string {
	return filepath.Join(s.cfg.DataDir, "seedance_tasks.json")
}

func (s *Store) loadSeedanceTaskMap() map[string]map[string]any {
	var data map[string]map[string]any
	if err := readJSON(s.seedanceTasksFile(), &data); err != nil {
		return map[string]map[string]any{}
	}
	if data == nil {
		return map[string]map[string]any{}
	}
	return data
}

func (s *Store) GetSeedanceTask(runID string) map[string]any {
	clean := strings.TrimSpace(runID)
	if clean == "" {
		return nil
	}
	s.seedanceMu.Lock()
	defer s.seedanceMu.Unlock()
	return cloneMap(s.loadSeedanceTaskMap()[clean])
}

func (s *Store) SaveSeedanceTask(record map[string]any) (map[string]any, error) {
	runID := strings.TrimSpace(stringValue(record["run_id"], ""))
	if runID == "" {
		return nil, ErrBadID
	}
	record = sanitizePersistedMap(record)
	s.seedanceMu.Lock()
	defer s.seedanceMu.Unlock()

	data := s.loadSeedanceTaskMap()
	existing := cloneMap(data[runID])
	if existing == nil {
		existing = map[string]any{"created_at": float64(NowMS()) / 1000.0}
	}
	for key, value := range record {
		if value != nil {
			existing[key] = value
		}
	}
	existing["run_id"] = runID
	existing["updated_at"] = float64(NowMS()) / 1000.0
	data[runID] = existing
	if err := writeJSON(s.seedanceTasksFile(), data); err != nil {
		return nil, err
	}
	return cloneMap(existing), nil
}

func (s *Store) UpdateSeedanceTask(runID string, updates map[string]any) (map[string]any, error) {
	clean := strings.TrimSpace(runID)
	if clean == "" {
		return nil, ErrBadID
	}
	updates = sanitizePersistedMap(updates)
	s.seedanceMu.Lock()
	defer s.seedanceMu.Unlock()

	data := s.loadSeedanceTaskMap()
	existing := cloneMap(data[clean])
	if existing == nil {
		return nil, ErrNotFound
	}
	for key, value := range updates {
		existing[key] = value
	}
	existing["updated_at"] = float64(NowMS()) / 1000.0
	data[clean] = existing
	if err := writeJSON(s.seedanceTasksFile(), data); err != nil {
		return nil, err
	}
	return cloneMap(existing), nil
}

func (s *Store) SeedanceClaimedTaskIDs() map[string]bool {
	s.seedanceMu.Lock()
	defer s.seedanceMu.Unlock()

	out := map[string]bool{}
	for _, record := range s.loadSeedanceTaskMap() {
		for _, id := range stringSlice(record["task_ids"]) {
			out[id] = true
		}
	}
	return out
}

func (s *Store) FindSeedanceRunByTaskID(taskID string) map[string]any {
	clean := strings.TrimSpace(taskID)
	if clean == "" {
		return nil
	}
	s.seedanceMu.Lock()
	defer s.seedanceMu.Unlock()

	for _, record := range s.loadSeedanceTaskMap() {
		for _, id := range stringSlice(record["task_ids"]) {
			if id == clean {
				return cloneMap(record)
			}
		}
	}
	return nil
}

func stringSlice(value any) []string {
	switch list := value.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if text := strings.TrimSpace(stringValue(item, "")); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, val := range value {
		out[key] = val
	}
	return out
}
