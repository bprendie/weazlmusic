package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

func validRadioURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("Use a public http or https stream URL")
	}
	return nil
}
func publicIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2002::/16", "64:ff9b::/96", "::/96"} {
		if netip.MustParsePrefix(cidr).Contains(a) {
			return false
		}
	}
	return true
}
func radioClient() *http.Client {
	tr := &http.Transport{ResponseHeaderTimeout: 15 * time.Second, TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 10}
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
			return nil, errors.New("Radio host not found")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("Radio streams must use public Internet addresses")
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		var last error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	return &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("Too many radio redirects")
		}
		return validRadioURL(req.URL.String())
	}}
}
