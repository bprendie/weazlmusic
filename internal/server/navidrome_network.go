package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

func navidromeTransport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.ResponseHeaderTimeout = 20 * time.Second
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("Navidrome host not found")
		}
		// LAN and loopback Navidrome servers are supported; metadata/link-local services are not.
		for _, ip := range ips {
			if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
				return nil, errors.New("Unsupported Navidrome network address")
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		var last error
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}
	return tr
}
