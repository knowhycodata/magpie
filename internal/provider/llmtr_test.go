package provider

import (
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
)

func TestLLMTRPreset(t *testing.T) {
	p, err := FromPreset("llmtr")
	if err != nil {
		t.Fatal(err)
	}
	if p.Chat != "https://llmtr.com/v1" || p.Responses != p.Chat || p.Anthropic != "https://llmtr.com" {
		t.Fatalf("endpoints: %q %q %q", p.Chat, p.Responses, p.Anthropic)
	}
	if p.Name != "LLMTR" || p.Icon != "llmtr-color" || p.KeysURL != "https://llmtr.com/dashboard/keys" {
		t.Fatalf("preset: %+v", p)
	}
	pr := Preset("llmtr")
	if pr.Kind != KindRelay || pr.NoKey || pr.NoList {
		t.Fatalf("kind %q noKey %v noList %v", pr.Kind, pr.NoKey, pr.NoList)
	}
	// an entry another app has at llmtr.com is the preset
	im, why := imported("LLMTR", "llmtr-x", endpoints{chat: "https://llmtr.com/v1"}, nil)
	if why != "" || im.Preset != "llmtr" || im.Icon != "llmtr-color" || im.Responses != p.Responses {
		t.Fatalf("imported: %q %+v", why, im)
	}
}

// LLMTR's own models are named llmtr/…, the same word as the preset.
// Asked by that id, the part after the first slash is not the model.
func TestLLMTRModelKeepsItsPrefix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	p, err := FromPreset("llmtr")
	if err != nil {
		t.Fatal(err)
	}
	p.Key = "llmtr-test"
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("llmtr", "", []catalog.Model{
		{ID: "llmtr/qwen3-8-27b"},
		{ID: "qwen/qwen3.8-27b-free"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ asked, model string }{
		{"llmtr/llmtr/qwen3-8-27b", "llmtr/qwen3-8-27b"},
		{"llmtr/qwen3-8-27b", "llmtr/qwen3-8-27b"},
		{"llmtr/qwen/qwen3.8-27b-free", "qwen/qwen3.8-27b-free"},
	} {
		got, model, ok := Resolve(tc.asked)
		if !ok || got.ID != "llmtr" || model != tc.model {
			t.Errorf("%s: %s %q ok=%v", tc.asked, got.ID, model, ok)
		}
	}
}
