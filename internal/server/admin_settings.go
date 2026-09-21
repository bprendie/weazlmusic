package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) adminConfig(w http.ResponseWriter, r *http.Request, se *session) {
	cfg, err := s.store.readGlobal()
	if err != nil {
		fail(w, 500, "Could not load installation settings")
		return
	}
	jsonOut(w, 200, map[string]any{
		"adminUsername": cfg.AdminUsername,
		"navidromeURL":  cfg.NavidromeURL,
		"llm":           visibleLLM(cfg.LLM),
	})
}

type adminConfigInput struct {
	NavidromeURL string  `json:"navidromeURL"`
	Provider     string  `json:"provider"`
	LLMURL       string  `json:"llmURL"`
	Model        string  `json:"model"`
	APIKey       *string `json:"apiKey"`
}

func readAdminLLM(in adminConfigInput, previous *llmConfig) (llmConfig, error) {
	if in.Provider == "off" || in.Provider == "" {
		return llmConfig{Provider: "off"}, nil
	}
	if in.Provider != "ollama" && in.Provider != "vllm" {
		return llmConfig{}, errors.New("choose Ollama or vLLM")
	}
	raw := strings.TrimRight(strings.TrimSpace(in.LLMURL), "/")
	if in.Provider == "ollama" {
		raw = strings.TrimSuffix(raw, "/api")
	} else {
		raw = strings.TrimSuffix(raw, "/v1")
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return llmConfig{}, errors.New("enter an http(s) LLM endpoint without credentials or query")
	}
	c := llmConfig{Provider: in.Provider, URL: raw, Model: strings.TrimSpace(in.Model)}
	if len(c.Model) > 200 || c.Model == "" {
		return llmConfig{}, errors.New("choose a model")
	}
	if in.APIKey != nil {
		if len(*in.APIKey) > 4096 {
			return llmConfig{}, errors.New("API key is too long")
		}
		c.APIKey = *in.APIKey
	} else if previous != nil && previous.URL == c.URL && previous.Provider == c.Provider {
		c.APIKey = previous.APIKey
	}
	return c, nil
}

func (s *Server) saveAdminConfig(w http.ResponseWriter, r *http.Request, se *session) {
	var in adminConfigInput
	if decode(r, &in) != nil {
		fail(w, 400, "Invalid installation settings")
		return
	}
	nav, err := validNavidromeURL(in.NavidromeURL)
	if err != nil {
		fail(w, 400, "Enter an http(s) Navidrome URL without credentials, query, or fragment")
		return
	}
	cfg, err := s.store.readGlobal()
	if err != nil {
		fail(w, 500, "Could not load installation settings")
		return
	}
	llm, err := readAdminLLM(in, cfg.LLM)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	cfg.NavidromeURL, cfg.LLM = nav, nil
	if llm.Provider != "off" {
		cfg.LLM = &llm
	}
	if err = s.store.writeGlobal(cfg); err != nil {
		fail(w, 500, "Could not save installation settings")
		return
	}
	jsonOut(w, 200, map[string]any{"navidromeURL": cfg.NavidromeURL, "llm": visibleLLM(cfg.LLM)})
}
