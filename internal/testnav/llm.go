package testnav

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// serveLLM streams deliberately fragmented IDs, with an invalid ID and a duplicate.
func serveLLM(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/tags":
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "fixture-model"}}})
		return true
	case "/v1/models":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "fixture-model"}}})
		return true
	case "/api/chat", "/v1/chat/completions":
	default:
		return false
	}
	var request struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Messages) < 2 {
		http.Error(w, "invalid", 400)
		return true
	}
	prompt := request.Messages[len(request.Messages)-1].Content
	start := strings.Index(prompt, "{")
	var payload struct {
		Candidates []struct {
			ID string `json:"id"`
		} `json:"candidates"`
	}
	if start < 0 || json.Unmarshal([]byte(prompt[start:]), &payload) != nil {
		http.Error(w, "invalid", 400)
		return true
	}
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(delta string) {
		var value any = map[string]any{"message": map[string]string{"content": delta}}
		if r.URL.Path == "/v1/chat/completions" {
			value = map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": delta}}}}
			fmt.Fprint(w, "data: ")
		}
		_ = json.NewEncoder(w).Encode(value)
		fmt.Fprint(w, "\n")
		w.(http.Flusher).Flush()
	}
	send(`["invented-id",`)
	for i, t := range payload.Candidates {
		select {
		case <-r.Context().Done():
			return true
		case <-time.After(60 * time.Millisecond):
		}
		id, _ := json.Marshal(t.ID)
		send(string(id[:2]))
		send(string(id[2:]) + ",")
		if i == 0 {
			send(string(id) + ",")
		}
	}
	send("]")
	return true
}
