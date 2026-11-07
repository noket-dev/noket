package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultDialTimeout           = 10 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultResponseHeaderTimeout = 300 * time.Second
	maxRedirects                 = 5
)

// ParseBaseURL validates the shape of an upstream address. It does not
// resolve DNS; use Resolve for that.
func ParseBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("base_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("base_url is invalid: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("base_url must use http or https")
	}
	if u.Hostname() == "" {
		return nil, errors.New("base_url must include a host")
	}
	if u.User != nil {
		return nil, errors.New("base_url must not include user information")
	}
	if u.RawQuery != "" || u.ForceQuery {
		return nil, errors.New("base_url must not include a query")
	}
	if u.Fragment != "" {
		return nil, errors.New("base_url must not include a fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

// JoinPath appends requestPath to basePath without duplicating a /v1 segment:
// a base URL written with or without the version prefix produces the same
// upstream path.
func JoinPath(basePath, requestPath string) string {
	base := strings.TrimRight(basePath, "/")
	if requestPath == "" {
		if base == "" {
			return "/"
		}
		return base
	}

	path := "/" + strings.TrimLeft(requestPath, "/")
	if strings.HasSuffix(base, "/v1") && (path == "/v1" || strings.HasPrefix(path, "/v1/")) {
		return base + strings.TrimPrefix(path, "/v1")
	}
	return base + path
}

// Resolve returns the addresses behind host without filtering them: loopback,
// private, link-local, metadata and public addresses are all accepted. The
// caller decides which address to dial.
func Resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}

	seen := make(map[string]struct{}, len(addrs))
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		key := addr.IP.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ips = append(ips, addr.IP)
	}
	return ips, nil
}

// ClientOptions configures the HTTP client used for probing and forwarding.
type ClientOptions struct {
	// ResponseHeaderTimeout bounds how long a local server may take to send
	// response headers. Zero uses the 300s tunnel default.
	ResponseHeaderTimeout time.Duration
}

// NewClient builds an HTTP client that resolves every dial itself, accepts any
// address, bypasses HTTP_PROXY / HTTPS_PROXY, and refuses cross-host redirects.
func NewClient(opts ClientOptions) *http.Client {
	headerTimeout := opts.ResponseHeaderTimeout
	if headerTimeout <= 0 {
		headerTimeout = defaultResponseHeaderTimeout
	}

	dialer := &net.Dialer{Timeout: defaultDialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := Resolve(ctx, host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, fmt.Errorf("dial %s: %w", host, lastErr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       defaultIdleConnTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
		ResponseHeaderTimeout: headerTimeout,
		// Forwarding must not add gzip or transparently decompress bodies; the
		// client and server negotiate compression end to end.
		DisableCompression: true,
	}

	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				return fmt.Errorf("refusing redirect to %s", req.URL.Host)
			}
			return nil
		},
	}
}
