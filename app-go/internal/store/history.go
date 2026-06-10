package store

import (
	"errors"
	"os"
	"sort"
)

func (s *Store) SaveHistory(record map[string]any) error {
	s.providerMu.Lock()
	defer s.providerMu.Unlock()

	var records []map[string]any
	if err := readJSON(s.cfg.HistoryFile, &records); err != nil && !errors.Is(err, os.ErrNotExist) {
		records = []map[string]any{}
	}
	if record["timestamp"] == nil {
		record["timestamp"] = float64(NowMS()) / 1000.0
	}
	next := make([]map[string]any, 0, len(records)+1)
	next = append(next, record)
	next = append(next, records...)
	if len(next) > 5000 {
		next = next[:5000]
	}
	return writeJSON(s.cfg.HistoryFile, next)
}

func (s *Store) LoadHistory(historyType string) []map[string]any {
	var records []map[string]any
	if err := readJSON(s.cfg.HistoryFile, &records); err != nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(records))
	for _, item := range records {
		if historyType != "" && stringValue(item["type"], "zimage") != historyType {
			continue
		}
		if lenArray(item["images"]) <= 0 {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return number(out[i]["timestamp"]) > number(out[j]["timestamp"])
	})
	return out
}

func (s *Store) DeleteHistory(timestamp float64) (map[string]any, error) {
	var records []map[string]any
	if err := readJSON(s.cfg.HistoryFile, &records); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	next := make([]map[string]any, 0, len(records))
	var target map[string]any
	for _, item := range records {
		itemTS := number(item["timestamp"])
		if almostSameTimestamp(itemTS, timestamp) {
			target = item
			continue
		}
		next = append(next, item)
	}
	if target != nil {
		if err := writeJSON(s.cfg.HistoryFile, next); err != nil {
			return nil, err
		}
	}
	return target, nil
}

func almostSameTimestamp(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.001
}
