package store

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var unsafeFilenamePattern = regexp.MustCompile(`[\\/:*?"<>|]+`)

func (s *Store) OutputStorage(category string) (string, string) {
	if category == "input" {
		return s.cfg.OutputInputDir, "input"
	}
	return s.cfg.OutputOutputDir, "output"
}

func (s *Store) OutputPathFor(filename, category string) string {
	folder, _ := s.OutputStorage(category)
	return filepath.Join(folder, filepath.Base(filename))
}

func (s *Store) OutputURLFor(filename, category string) string {
	_, subdir := s.OutputStorage(category)
	return "/assets/" + subdir + "/" + path.Base(filename)
}

func (s *Store) OutputFileFromURL(raw any) string {
	text, ok := raw.(string)
	if !ok {
		if item, ok := raw.(map[string]any); ok {
			text = stringValue(item["url"], "")
		}
	}
	if text == "" || (!strings.HasPrefix(text, "/output/") && !strings.HasPrefix(text, "/assets/")) {
		return ""
	}
	clean := strings.SplitN(text, "?", 2)[0]
	if decoded, err := url.PathUnescape(clean); err == nil {
		clean = decoded
	}
	clean = strings.ReplaceAll(clean, "\\", "/")

	root := s.cfg.OutputDir
	rel := strings.TrimPrefix(clean, "/output/")
	if strings.HasPrefix(clean, "/assets/") {
		root = s.cfg.AssetsDir
		rel = strings.TrimPrefix(clean, "/assets/")
	}
	rel = strings.TrimLeft(rel, "/")
	if rel == "" {
		return ""
	}

	path := filepath.Join(root, filepath.FromSlash(rel))
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	relToRoot, err := filepath.Rel(absRoot, absPath)
	if err != nil || strings.HasPrefix(relToRoot, "..") || filepath.IsAbs(relToRoot) {
		return ""
	}
	if stat, err := os.Stat(absPath); err != nil || stat.IsDir() {
		return ""
	}
	return absPath
}

func ContentTypeForPath(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".aac":
		return "audio/aac"
	case ".flac":
		return "audio/flac"
	case ".ogg":
		return "audio/ogg"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".zip":
		return "application/zip"
	default:
		return "image/png"
	}
}

func SafeArchiveName(name, fallback string) string {
	cleaned := unsafeFilenamePattern.ReplaceAllString(name, "_")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return fallback
	}
	return cleaned
}
