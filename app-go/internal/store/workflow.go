package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var workflowNamePattern = regexp.MustCompile(`^(?:(?:custom|自定义)/)?[a-zA-Z0-9_\p{Han}\.\-]+\.json$`)

var builtinWorkflows = map[string]bool{
	"Z-Image.json":         true,
	"Z-Image-Enhance.json": true,
	"2511.json":            true,
	"klein-enhance.json":   true,
	"Flux2-Klein.json":     true,
	"upscale.json":         true,
}

func (s *Store) ListWorkflows() []map[string]any {
	root := s.cfg.WorkflowDir
	if stat, err := os.Stat(root); err != nil || !stat.IsDir() {
		return []map[string]any{}
	}
	items := make([]map[string]any, 0)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			name := entry.Name()
			if filepath.Dir(path) == root && name != "custom" && name != "自定义" {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".config.json") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isBuiltinWorkflow(rel) {
			return nil
		}
		cfg := s.workflowConfig(rel)
		items = append(items, map[string]any{
			"name":        rel,
			"title":       stringValue(cfg["title"], strings.TrimSuffix(name, ".json")),
			"builtin":     false,
			"field_count": lenArray(cfg["fields"]),
		})
		return nil
	})
	sort.Slice(items, func(i, j int) bool {
		ai := sortWorkflowKey(stringValue(items[i]["name"], ""), stringValue(items[i]["title"], ""))
		aj := sortWorkflowKey(stringValue(items[j]["name"], ""), stringValue(items[j]["title"], ""))
		return ai < aj
	})
	return items
}

func (s *Store) GetWorkflow(name string) (map[string]any, error) {
	if !workflowNamePattern.MatchString(name) {
		return nil, ErrBadID
	}
	path, ok := s.workflowPath(name)
	if !ok {
		return nil, ErrBadID
	}
	workflow, err := readJSONMap(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	cfg := map[string]any{"title": strings.TrimSuffix(name, ".json"), "fields": []any{}}
	for key, value := range s.workflowConfig(name) {
		cfg[key] = value
	}
	return map[string]any{
		"name":     name,
		"workflow": workflow,
		"config":   cfg,
		"builtin":  isBuiltinWorkflow(name),
	}, nil
}

func (s *Store) SaveWorkflow(name string, workflow map[string]any) (string, error) {
	clean := filepath.Base(strings.TrimSpace(name))
	if !strings.HasSuffix(clean, ".json") {
		clean += ".json"
	}
	if !workflowNamePattern.MatchString(clean) || len(workflow) == 0 {
		return "", ErrBadID
	}
	sampleOK := false
	for _, value := range workflow {
		if node, ok := value.(map[string]any); ok && node["class_type"] != nil {
			sampleOK = true
			break
		}
	}
	if !sampleOK {
		return "", errors.New("不是有效的 ComfyUI API 工作流 JSON（需包含 class_type）")
	}
	storedName := "custom/" + clean
	path, ok := s.workflowPath(storedName)
	if !ok {
		return "", ErrBadID
	}
	if err := writeJSON(path, workflow); err != nil {
		return "", err
	}
	return storedName, nil
}

func (s *Store) SaveWorkflowConfig(name string, cfg map[string]any) error {
	if !workflowNamePattern.MatchString(name) {
		return ErrBadID
	}
	path, ok := s.workflowPath(name)
	if !ok {
		return ErrBadID
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	cfgPath, ok := s.workflowConfigPath(name)
	if !ok {
		return ErrBadID
	}
	return writeJSON(cfgPath, cfg)
}

func (s *Store) DeleteWorkflow(name string) error {
	if !workflowNamePattern.MatchString(name) {
		return ErrBadID
	}
	if isBuiltinWorkflow(name) {
		return errors.New("内置工作流不可删除")
	}
	path, ok := s.workflowPath(name)
	if !ok {
		return ErrBadID
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if cfgPath, ok := s.workflowConfigPath(name); ok {
		_ = os.Remove(cfgPath)
	}
	return nil
}

func (s *Store) workflowConfig(name string) map[string]any {
	cfgPath, ok := s.workflowConfigPath(name)
	if !ok {
		return map[string]any{}
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return map[string]any{}
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return map[string]any{}
	}
	if cfg == nil {
		return map[string]any{}
	}
	return cfg
}

func (s *Store) workflowPath(name string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(name), "/")
	path := filepath.Join(append([]string{s.cfg.WorkflowDir}, parts...)...)
	root, err := filepath.Abs(s.cfg.WorkflowDir)
	if err != nil {
		return "", false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", false
	}
	return abs, true
}

func (s *Store) workflowConfigPath(name string) (string, bool) {
	path, ok := s.workflowPath(name)
	if !ok {
		return "", false
	}
	return strings.TrimSuffix(path, ".json") + ".config.json", true
}

func isBuiltinWorkflow(name string) bool {
	return !strings.Contains(name, "/") && builtinWorkflows[filepath.Base(name)]
}

func sortWorkflowKey(name, title string) string {
	prefix := "1:"
	if strings.HasPrefix(name, "custom/") {
		prefix = "0:"
	}
	return prefix + title
}
