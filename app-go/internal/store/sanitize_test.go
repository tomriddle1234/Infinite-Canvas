package store

import "testing"

func TestSanitizePersistedMapRedactsSignedURLsAndAccessKeys(t *testing.T) {
	input := map[string]any{
		"run_id":     "run-1",
		"signed_url": "https://example.test/video.mp4?x-tos-signature=secret&keep=value",
		"message":    "temporary key AKLTABCDEF123456 is hidden",
		"nested": []any{
			"https://example.test/file.png?X-Tos-Security-Token=token",
		},
	}

	got := sanitizePersistedMap(input)

	if got["signed_url"] != "https://example.test/video.mp4?keep=value&x-tos-signature=%2A%2A%2A" {
		t.Fatalf("signed URL was not redacted: %v", got["signed_url"])
	}
	if got["message"] != "temporary key AKLT*** is hidden" {
		t.Fatalf("access key was not redacted: %v", got["message"])
	}
	nested, ok := got["nested"].([]any)
	if !ok || len(nested) != 1 {
		t.Fatalf("nested value shape changed: %#v", got["nested"])
	}
	if nested[0] != "https://example.test/file.png?X-Tos-Security-Token=%2A%2A%2A" {
		t.Fatalf("nested signed URL was not redacted: %v", nested[0])
	}
}
