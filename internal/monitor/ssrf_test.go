package monitor

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

type dialerFunc func(context.Context, string, string) (net.Conn, error)

func (f dialerFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

func TestSafeDialerRejectsForbiddenResolvedAddressesBeforeDial(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.4", "169.254.169.254", "100.64.0.1", "192.0.2.10", "::1", "fe80::1", "fd00::1"} {
		t.Run(raw, func(t *testing.T) {
			calls := 0
			d := NewSafeDialer(resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr(raw)}, nil
			}), dialerFunc(func(context.Context, string, string) (net.Conn, error) { calls++; return nil, nil }), nil)
			if _, err := d.DialContext(context.Background(), "tcp", "probe.invalid:80"); err == nil {
				t.Fatal("DialContext accepted forbidden address")
			}
			if calls != 0 {
				t.Fatalf("underlying dialer called %d times for forbidden address", calls)
			}
		})
	}
}

func TestSafeDialerPinsValidatedAddressAndRejectsDNSRebinding(t *testing.T) {
	var mu sync.Mutex
	answers := []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")}
	lookups := 0
	dials := 0
	resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		mu.Lock()
		defer mu.Unlock()
		value := answers[lookups]
		lookups++
		return []netip.Addr{value}, nil
	})
	d := NewSafeDialer(resolver, dialerFunc(func(ctx context.Context, network, address string) (net.Conn, error) {
		dials++
		if address != "93.184.216.34:443" {
			t.Fatalf("dialed %q; want pinned validated IP", address)
		}
		left, right := net.Pipe()
		go right.Close()
		return left, nil
	}), nil)
	conn, err := d.DialContext(context.Background(), "tcp", "probe.invalid:443")
	if err != nil {
		t.Fatalf("first public resolution: %v", err)
	}
	conn.Close()
	if _, err = d.DialContext(context.Background(), "tcp", "probe.invalid:443"); err == nil {
		t.Fatal("second resolution accepted rebinding to loopback")
	}
	if dials != 1 {
		t.Fatalf("dialer calls=%d want 1", dials)
	}
}

func TestSafeDialerAllowsOnlyExplicitlyConfiguredPrivateCIDRs(t *testing.T) {
	resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.8.0.14")}, nil
	})
	calls := 0
	dialer := dialerFunc(func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("dial attempted")
	})
	denied := NewSafeDialer(resolver, dialer, nil)
	if _, err := denied.DialContext(context.Background(), "tcp", "service.internal:8080"); err == nil {
		t.Fatal("private address accepted without policy")
	}
	allowed := NewSafeDialer(resolver, dialer, []netip.Prefix{netip.MustParsePrefix("10.8.0.0/24")})
	if _, err := allowed.DialContext(context.Background(), "tcp", "service.internal:8080"); err == nil || calls != 1 {
		t.Fatalf("explicit policy did not reach dialer: err=%v calls=%d", err, calls)
	}
}

func TestValidateHTTPURLRejectsUnsupportedSchemesCredentialsAndMalformedTargets(t *testing.T) {
	for _, target := range []string{"file:///etc/passwd", "ftp://example.com", "http://user:pass@example.com", "http://", "http://example.com/#fragment", "gopher://127.0.0.1:70"} {
		if _, err := validateHTTPURL(target); err == nil {
			t.Errorf("validateHTTPURL(%q) accepted unsafe target", target)
		}
	}
	if _, err := validateHTTPURL("https://example.com/health"); err != nil {
		t.Fatalf("valid HTTPS URL rejected: %v", err)
	}
}

func TestParseAllowedCIDRsAcceptsOnlyNarrowPrivateSubnets(t *testing.T) {
	if prefixes, err := ParseAllowedCIDRs([]string{"10.8.0.0/24", "fc00::/8"}); err != nil || len(prefixes) != 2 {
		t.Fatalf("ParseAllowedCIDRs(private) = %v, %v", prefixes, err)
	}
	for _, value := range []string{"0.0.0.0/0", "10.0.0.0/7", "100.64.0.0/10", "192.0.2.0/24", "2001:db8::/32"} {
		if _, err := ParseAllowedCIDRs([]string{value}); err == nil {
			t.Errorf("ParseAllowedCIDRs(%q) accepted non-private or overly broad network", value)
		}
	}
}

func TestHTTPProbeHonorsContextTimeoutAndBoundsBody(t *testing.T) {
	slow := &httpClientFunc{do: func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}}
	probe := HTTPProbe{Client: slow, MaxBodyBytes: 8}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if _, err := probe.Check(ctx, "https://example.com/health"); err == nil {
		t.Fatal("HTTP probe ignored context timeout")
	}
	oversized := HTTPProbe{Client: &httpClientFunc{do: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("123456789"))}, nil
	}}, MaxBodyBytes: 8}
	if _, err := oversized.Check(context.Background(), "https://example.com"); err == nil {
		t.Fatal("HTTP probe accepted an oversized response body")
	}
}

func TestHTTPProbeDoesNotFollowRedirects(t *testing.T) {
	requests := 0
	probe := HTTPProbe{Client: &httpClientFunc{do: func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"http://127.0.0.1/admin"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
	}}}
	result, err := probe.Check(context.Background(), "https://public.example/health")
	if err == nil || result.OK || result.StatusCode != http.StatusFound || requests != 1 {
		t.Fatalf("redirect probe result=%+v err=%v requests=%d", result, err, requests)
	}
}

type httpClientFunc struct {
	do func(*http.Request) (*http.Response, error)
}

func (f *httpClientFunc) Do(r *http.Request) (*http.Response, error) { return f.do(r) }
