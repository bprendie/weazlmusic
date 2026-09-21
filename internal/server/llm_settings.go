package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type llmConfig struct {
	Provider string `json:"provider"`
	URL      string `json:"url"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey,omitempty"`
}
type llmInput struct {
	Provider string  `json:"provider"`
	URL      string  `json:"url"`
	Model    string  `json:"model"`
	APIKey   *string `json:"apiKey"`
}

func visibleLLM(c *llmConfig) map[string]any {
	if c == nil {
		return map[string]any{"provider": "off", "url": "", "model": "", "hasKey": false}
	}
	return map[string]any{"provider": c.Provider, "url": c.URL, "model": c.Model, "hasKey": c.APIKey != ""}
}
func (s *Server) effectiveLLM(se *session) (*llmConfig, error) {
	a, err := s.loadAccount(se.User)
	if err != nil {
		return nil, err
	}
	if a.LLM != nil {
		return a.LLM, nil
	}
	cfg, err := s.store.readGlobal()
	if err != nil {
		return nil, err
	}
	return cfg.LLM, nil
}
func (s *Server) llmSettings(w http.ResponseWriter, r *http.Request, se *session) {
	c, err := s.effectiveLLM(se)
	if err != nil {
		fail(w, 500, "Could not load settings")
		return
	}
	jsonOut(w, 200, visibleLLM(c))
}
func (s *Server) readLLMInput(r *http.Request, se *session) (llmConfig, error) {
	var in llmInput
	if decode(r, &in) != nil {
		return llmConfig{}, errors.New("Invalid curator settings")
	}
	if in.Provider == "off" {
		return llmConfig{Provider: "off"}, nil
	}
	if in.Provider != "ollama" && in.Provider != "vllm" {
		return llmConfig{}, errors.New("Choose Ollama or vLLM")
	}
	raw := strings.TrimRight(strings.TrimSpace(in.URL), "/")
	if in.Provider == "ollama" {
		raw = strings.TrimSuffix(raw, "/api")
	} else {
		raw = strings.TrimSuffix(raw, "/v1")
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return llmConfig{}, errors.New("Enter an http(s) LLM endpoint without credentials or query")
	}
	c := llmConfig{Provider: in.Provider, URL: raw, Model: strings.TrimSpace(in.Model)}
	if len(c.Model) > 200 {
		return c, errors.New("Model name is too long")
	}
	if in.APIKey != nil {
		if len(*in.APIKey) > 4096 {
			return c, errors.New("API key is too long")
		}
		c.APIKey = *in.APIKey
	} else {
		a, e := s.loadAccount(se.User)
		if e != nil {
			return c, e
		}
		if a.LLM != nil && a.LLM.URL == c.URL && a.LLM.Provider == c.Provider {
			c.APIKey = a.LLM.APIKey
		}
	}
	return c, nil
}
func (s *Server) llmModels(ctx context.Context, c llmConfig) ([]string, error) {
	path := "/v1/models"
	if c.Provider == "ollama" {
		path = "/api/tags"
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", c.URL+path, nil)
	if err != nil {
		return nil, errors.New("Invalid LLM endpoint")
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	res, err := s.upstream.Do(req)
	if err != nil {
		return nil, errors.New("LLM endpoint is unreachable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errors.New("LLM endpoint rejected model discovery; check the URL and API key")
	}
	var data struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&data) != nil {
		return nil, errors.New("Invalid model list")
	}
	out := []string{}
	for _, m := range data.Models {
		if m.Name != "" {
			out = append(out, m.Name)
		}
	}
	for _, m := range data.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}
func (s *Server) discoverLLM(w http.ResponseWriter, r *http.Request, se *session) {
	c, err := s.readLLMInput(r, se)
	if err != nil || c.Provider == "off" {
		fail(w, 400, "Choose a provider and endpoint first")
		return
	}
	models, err := s.llmModels(r.Context(), c)
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	jsonOut(w, 200, map[string]any{"models": models})
}
func (s *Server) saveLLM(w http.ResponseWriter, r *http.Request, se *session) {
	c, err := s.readLLMInput(r, se)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if c.Provider != "off" {
		if c.Model == "" {
			fail(w, 400, "Choose a model")
			return
		}
		models, err := s.llmModels(r.Context(), c)
		if err != nil {
			fail(w, 422, err.Error())
			return
		}
		found := false
		for _, m := range models {
			if m == c.Model {
				found = true
			}
		}
		if !found {
			fail(w, 422, "That model is not available at this endpoint")
			return
		}
	}
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	a, err := s.loadAccount(se.User)
	if err != nil {
		fail(w, 500, "Could not load account")
		return
	}
	a.LLM = &c
	if c.Provider == "off" {
		a.LLM = nil
	}
	if s.store.writeRecord("account:"+se.User, a) != nil {
		fail(w, 500, "Could not save curator settings")
		return
	}
	jsonOut(w, 200, visibleLLM(a.LLM))
}
