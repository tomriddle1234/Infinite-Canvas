package upstream

import "testing"

func TestNormalizePresetVirtualAssetUsesNestedMediaFields(t *testing.T) {
	item := map[string]any{
		"SID": "group-sid",
		"AssetGroup": map[string]any{
			"Name":     "Virtual Person",
			"CoverURL": "tos://not-browser-usable",
			"Content": map[string]any{
				"Image": []any{
					map[string]any{
						"AssetURI": "asset://asset-nested-123",
						"CoverURL": "https://example.com/cover.jpg",
					},
				},
			},
		},
	}

	asset := normalizePresetVirtualAsset(item)
	if got := asset["asset_id"]; got != "asset-nested-123" {
		t.Fatalf("asset_id = %v, want asset-nested-123", got)
	}
	if got := asset["asset_uri"]; got != "asset://asset-nested-123" {
		t.Fatalf("asset_uri = %v, want asset://asset-nested-123", got)
	}
	if got := asset["preview_url"]; got != "https://example.com/cover.jpg" {
		t.Fatalf("preview_url = %v, want nested cover URL", got)
	}
}

func TestNormalizePresetVirtualAssetFindsRecursivePreview(t *testing.T) {
	item := map[string]any{
		"AssetGroup": map[string]any{
			"Content": map[string]any{
				"Images": []any{
					map[string]any{
						"AssetID": "asset-from-images",
						"Meta": map[string]any{
							"ThumbnailURL": "//example.com/thumb.webp",
						},
					},
				},
			},
		},
	}

	asset := normalizePresetVirtualAsset(item)
	if got := asset["asset_id"]; got != "asset-from-images" {
		t.Fatalf("asset_id = %v, want asset-from-images", got)
	}
	if got := asset["preview_url"]; got != "https://example.com/thumb.webp" {
		t.Fatalf("preview_url = %v, want protocol-normalized thumbnail URL", got)
	}
}
