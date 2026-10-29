package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
)

// llmtr plays the gateway's own endpoints: chat completions and Responses
// under /v1, Anthropic messages at the root.
type llmtrUp struct {
	mu    sync.Mutex
	calls []llmtrCall
}

type llmtrCall struct {
	path, model string
	head        http.Header
}

func (q *llmtrUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var v struct {
		Model string `json:"model"`
	}
	json.Unmarshal(body, &v)
	q.mu.Lock()
	q.calls = append(q.calls, llmtrCall{r.URL.Path, v.Model, r.Header.Clone()})
	q.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	switch r.URL.Path {
	case "/v1/chat/completions":
		io.WriteString(w, sse(
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"from chat"}}]}`,
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
			`data: [DONE]`))
	case "/v1/responses":
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"resp_1"}}`,
			`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`,
			`data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"from responses"}`,
			`data: {"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":5,"output_tokens":2}}}`))
	case "/v1/messages":
		io.WriteString(w, sse(
			`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","model":"`+v.Model+`","usage":{"input_tokens":5}}}`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"from messages"}}`,
			`data: {"type":"content_block_stop","index":0}`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			`data: {"type":"message_stop"}`))
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"no such route"}`)
	}
}

func (q *llmtrUp) last() llmtrCall {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.calls) == 0 {
		return llmtrCall{}
	}
	return q.calls[len(q.calls)-1]
}

// LLMTR at a fake gateway. Model ids that start with llmtr/ keep that
// prefix on the way out, whether the client named the catalog id or the
// vendor's own. A request stays on the API the client spoke.
func TestLLMTRRoutes(t *testing.T) {
	fresh(t)
	up := &llmtrUp{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	p, err := provider.FromPreset("llmtr")
	if err != nil {
		t.Fatal(err)
	}
	p.Key = "llmtr-test"
	p.Chat, p.Responses = srv.URL+"/v1", srv.URL+"/v1"
	p.Anthropic = srv.URL
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("llmtr", "", []catalog.Model{
		{ID: "llmtr/qwen3-8-27b"},
		{ID: "qwen/qwen3.8-27b-free"},
		{ID: "openai/gpt-6-luna"},
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, path, body, path2, model, reply string
		bearer                                bool
	}{
		{"own model by catalog id", "/v1/chat/completions",
			`{"model":"llmtr/llmtr/qwen3-8-27b","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			"/v1/chat/completions", "llmtr/qwen3-8-27b", "from chat", true},
		{"own model by vendor id", "/v1/chat/completions",
			`{"model":"llmtr/qwen3-8-27b","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			"/v1/chat/completions", "llmtr/qwen3-8-27b", "from chat", true},
		{"free model", "/v1/chat/completions",
			`{"model":"llmtr/qwen/qwen3.8-27b-free","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			"/v1/chat/completions", "qwen/qwen3.8-27b-free", "from chat", true},
		{"responses", "/v1/responses",
			`{"model":"llmtr/openai/gpt-6-luna","input":"hi","stream":true}`,
			"/v1/responses", "openai/gpt-6-luna", "from responses", true},
		{"messages", "/v1/messages",
			`{"model":"llmtr/qwen/qwen3.8-27b-free","max_tokens":20,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			"/v1/messages", "qwen/qwen3.8-27b-free", "from messages", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := post(t, tc.path, tc.body)
			if code != 200 || !strings.Contains(body, tc.reply) {
				t.Fatalf("status %d: %s", code, body)
			}
			c := up.last()
			if c.path != tc.path2 || c.model != tc.model {
				t.Fatalf("upstream: %s %q", c.path, c.model)
			}
			if tc.bearer {
				if c.head.Get("Authorization") != "Bearer llmtr-test" {
					t.Fatalf("headers: %v", c.head)
				}
			} else if c.head.Get("x-api-key") != "llmtr-test" || c.head.Get("anthropic-version") != "2023-06-01" {
				t.Fatalf("headers: %v", c.head)
			}
		})
	}
}
