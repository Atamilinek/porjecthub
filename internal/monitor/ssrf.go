package monitor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultMaxResponseBytes = 64 * 1024

var absoluteBlocked = mustPrefixes(
	"0.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "192.0.0.0/24", "192.0.2.0/24",
	"192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20",
)

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}
type ContextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}
type SafeDialer struct {
	resolver Resolver
	dialer   ContextDialer
	allowed  []netip.Prefix
}

func NewSafeDialer(resolver Resolver, dialer ContextDialer, allowed []netip.Prefix) *SafeDialer {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	}
	return &SafeDialer{resolver: resolver, dialer: dialer, allowed: append([]netip.Prefix(nil), allowed...)}
}

func (d *SafeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("unsupported network %q", network)
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid probe address: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("invalid probe port")
	}
	var addresses []netip.Addr
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		addresses = []netip.Addr{ip}
	} else {
		addresses, err = d.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve probe target: %w", err)
		}
	}
	if len(addresses) == 0 || len(addresses) > 64 {
		return nil, errors.New("probe target resolved to an invalid number of addresses")
	}
	normalized := make([]netip.Addr, 0, len(addresses))
	for _, ip := range addresses {
		if !d.allowedAddress(ip) {
			return nil, fmt.Errorf("probe destination %s is outside permitted networks", ip)
		}
		normalized = append(normalized, ip)
	}
	dialAddress := net.JoinHostPort(normalized[0].String(), strconv.Itoa(port))
	return d.dialer.DialContext(ctx, network, dialAddress)
}

func (d *SafeDialer) allowedAddress(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" {
		return false
	}
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	for _, prefix := range absoluteBlocked {
		if prefix.Contains(ip) {
			return false
		}
	}
	if ip.IsPrivate() {
		for _, prefix := range d.allowed {
			if prefix.IsValid() && prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	return ip.IsGlobalUnicast() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsMulticast() && !ip.IsUnspecified()
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type ProbeResult struct {
	OK         bool
	StatusCode int
	Latency    time.Duration
	Detail     string
}
type HTTPProbe struct {
	Client       HTTPDoer
	MaxBodyBytes int64
}

func NewHTTPClient(allowed []netip.Prefix) *http.Client {
	transport := &http.Transport{Proxy: nil, DialContext: NewSafeDialer(nil, nil, allowed).DialContext, ForceAttemptHTTP2: false, MaxIdleConns: 20, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, ExpectContinueTimeout: time.Second}
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (p HTTPProbe) Check(ctx context.Context, target string) (ProbeResult, error) {
	parsed, err := validateHTTPURL(target)
	if err != nil {
		return ProbeResult{}, err
	}
	client := p.Client
	if client == nil {
		client = NewHTTPClient(nil)
	}
	maxBytes := p.MaxBodyBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxResponseBytes
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("create health probe: %w", err)
	}
	request.Header.Set("User-Agent", "ProjectHub-HealthCheck/1")
	started := time.Now()
	response, err := client.Do(request)
	latency := time.Since(started)
	if err != nil {
		return ProbeResult{Latency: latency, Detail: "request failed"}, err
	}
	if response == nil || response.Body == nil {
		return ProbeResult{Latency: latency}, errors.New("probe returned no response body")
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if readErr != nil {
		return ProbeResult{StatusCode: response.StatusCode, Latency: latency}, fmt.Errorf("read health response: %w", readErr)
	}
	if int64(len(body)) > maxBytes {
		return ProbeResult{StatusCode: response.StatusCode, Latency: latency, Detail: "response exceeded size limit"}, errors.New("health response exceeded size limit")
	}
	result := ProbeResult{OK: response.StatusCode >= 200 && response.StatusCode < 300, StatusCode: response.StatusCode, Latency: latency}
	if !result.OK {
		result.Detail = fmt.Sprintf("HTTP status %d", response.StatusCode)
		return result, fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
	}
	return result, nil
}

func validateHTTPURL(target string) (*url.URL, error) {
	if len(target) == 0 || len(target) > 2048 || strings.ContainsAny(target, "\r\n\x00") {
		return nil, errors.New("invalid HTTP probe URL")
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, errors.New("invalid HTTP probe URL")
	}
	if !parsed.IsAbs() || parsed.Opaque != "" || parsed.User != nil || parsed.Hostname() == "" || parsed.Fragment != "" || parsed.RawFragment != "" {
		return nil, errors.New("invalid HTTP probe URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("unsupported HTTP probe scheme")
	}
	if parsed.Port() != "" {
		port, err := strconv.Atoi(parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, errors.New("invalid HTTP probe port")
		}
	}
	return parsed, nil
}

func mustPrefixes(values ...string) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}
