package server

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Origin-specific transports keep private-network permission scoped across
// redirects, playlists and connection reuse. DNS answers are checked at dial.
type radioOriginTransport struct {
	allowed         map[string]bool
	public, private *http.Transport
}

func (t *radioOriginTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := validRadioURL(r.URL.String()); err != nil {
		return nil, err
	}
	if t.allowed[radioOrigin(r.URL)] {
		return t.private.RoundTrip(r)
	}
	return t.public.RoundTrip(r)
}

func (t *radioOriginTransport) CloseIdleConnections() {
	t.public.CloseIdleConnections()
	t.private.CloseIdleConnections()
}

func radioOrigin(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return u.Scheme + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

func parseRadioOrigins(raw string) ([]string, error) {
	var origins []string
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		u, err := url.Parse(entry)
		if err != nil || validRadioURL(entry) != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(u.Host, "*") {
			return nil, errors.New("RADIO_PRIVATE_ORIGINS must contain comma-separated http(s) origins without credentials, paths, queries or wildcards")
		}
		origins = append(origins, radioOrigin(u))
	}
	return origins, nil
}
