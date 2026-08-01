package helps

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	tls "github.com/refraction-networking/utls"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
	"golang.org/x/net/proxy"
)

// utlsConnectTimeout bounds connection establishment only (TCP dial, TLS
// handshake and the HTTP/2 preface). It is deliberately not applied to any
// network activity after the upstream connection is usable: the raw connection
// deadline is cleared once the HTTP/2 client connection exists, so streaming
// responses remain unbounded per AGENTS.md.
const utlsConnectTimeout = 30 * time.Second

// utlsRoundTripper implements http.RoundTripper using utls with Chrome fingerprint
// to bypass Cloudflare's TLS fingerprinting on Anthropic domains.
type utlsRoundTripper struct {
	mu          sync.Mutex
	connections map[string]*http2.ClientConn
	// pending tracks in-flight connection creation per host. A channel is used
	// instead of sync.Cond so waiters can also observe context cancellation;
	// sync.Cond offers no way to abandon a Wait.
	pending map[string]chan struct{}
	dialer  proxy.Dialer
}

func newUtlsRoundTripper(proxyURL string) *utlsRoundTripper {
	var dialer proxy.Dialer = proxy.Direct
	if proxyURL != "" {
		proxyDialer, mode, errBuild := proxyutil.BuildDialer(proxyURL)
		if errBuild != nil {
			log.Errorf("utls: failed to configure proxy dialer for %q: %v", proxyutil.Redact(proxyURL), errBuild)
		} else if mode != proxyutil.ModeInherit && proxyDialer != nil {
			dialer = proxyDialer
		}
	}
	return &utlsRoundTripper{
		connections: make(map[string]*http2.ClientConn),
		pending:     make(map[string]chan struct{}),
		dialer:      dialer,
	}
}

func (t *utlsRoundTripper) getOrCreateConnection(ctx context.Context, host, addr string) (*http2.ClientConn, error) {
	for {
		t.mu.Lock()
		if h2Conn, ok := t.connections[host]; ok && h2Conn.CanTakeNewRequest() {
			t.mu.Unlock()
			return h2Conn, nil
		}

		if done, ok := t.pending[host]; ok {
			t.mu.Unlock()
			select {
			case <-done:
				// The in-flight attempt finished. Re-evaluate from the top: either it
				// published a usable connection, or this caller becomes the next
				// creator. Waiters no longer dial concurrently on failure.
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		done := make(chan struct{})
		t.pending[host] = done
		t.mu.Unlock()

		h2Conn, err := t.createConnection(ctx, host, addr)

		t.mu.Lock()
		delete(t.pending, host)
		if err == nil {
			t.connections[host] = h2Conn
		}
		t.mu.Unlock()
		close(done)

		if err != nil {
			return nil, err
		}
		return h2Conn, nil
	}
}

// dialContext dials addr while honouring ctx. proxy.Dialer has no context-aware
// method, so a ContextDialer is preferred and the plain Dial is otherwise run in
// a goroutine that can be abandoned; without this a blackholed route blocks the
// caller (and every cond waiter behind it) forever.
func (t *utlsRoundTripper) dialContext(ctx context.Context, addr string) (net.Conn, error) {
	if contextDialer, ok := t.dialer.(proxy.ContextDialer); ok {
		return contextDialer.DialContext(ctx, "tcp", addr)
	}

	type dialResult struct {
		conn net.Conn
		err  error
	}
	resultCh := make(chan dialResult, 1)
	go func() {
		conn, err := t.dialer.Dial("tcp", addr)
		resultCh <- dialResult{conn: conn, err: err}
	}()

	select {
	case <-ctx.Done():
		go func() {
			if abandoned := <-resultCh; abandoned.conn != nil {
				if errClose := abandoned.conn.Close(); errClose != nil {
					log.Debugf("utls: close abandoned connection to %s error: %v", addr, errClose)
				}
			}
		}()
		return nil, ctx.Err()
	case result := <-resultCh:
		return result.conn, result.err
	}
}

func (t *utlsRoundTripper) createConnection(ctx context.Context, host, addr string) (*http2.ClientConn, error) {
	conn, err := t.dialContext(ctx, addr)
	if err != nil {
		return nil, err
	}

	// Bound the handshake and HTTP/2 preface. The deadline is removed again as
	// soon as the connection is usable so request/response streaming stays
	// unbounded.
	if deadline, ok := ctx.Deadline(); ok {
		if errDeadline := conn.SetDeadline(deadline); errDeadline != nil {
			log.Debugf("utls: set handshake deadline for %s error: %v", addr, errDeadline)
		}
	}

	tlsConfig := &tls.Config{ServerName: host}
	tlsConn := tls.UClient(conn, tlsConfig, tls.HelloChrome_Auto)

	if err = tlsConn.Handshake(); err != nil {
		if errClose := conn.Close(); errClose != nil {
			log.Debugf("utls: close connection after handshake failure for %s error: %v", addr, errClose)
		}
		return nil, err
	}

	tr := &http2.Transport{}
	h2Conn, err := tr.NewClientConn(tlsConn)
	if err != nil {
		if errClose := tlsConn.Close(); errClose != nil {
			log.Debugf("utls: close connection after http2 setup failure for %s error: %v", addr, errClose)
		}
		return nil, err
	}

	if errDeadline := conn.SetDeadline(time.Time{}); errDeadline != nil {
		log.Debugf("utls: clear handshake deadline for %s error: %v", addr, errDeadline)
	}

	return h2Conn, nil
}

func (t *utlsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	hostname := req.URL.Hostname()
	port := req.URL.Port()
	if port == "" {
		port = "443"
	}
	addr := net.JoinHostPort(hostname, port)

	// Bound connection establishment only. The deadline set from this context is
	// cleared once the HTTP/2 client connection is usable, so the request and its
	// streaming response stay unbounded per AGENTS.md.
	connectCtx, cancel := context.WithTimeout(req.Context(), utlsConnectTimeout)
	defer cancel()

	h2Conn, err := t.getOrCreateConnection(connectCtx, hostname, addr)
	if err != nil {
		return nil, err
	}

	resp, err := h2Conn.RoundTrip(req)
	if err != nil {
		t.mu.Lock()
		if cached, ok := t.connections[hostname]; ok && cached == h2Conn {
			delete(t.connections, hostname)
		}
		t.mu.Unlock()
		return nil, err
	}

	return resp, nil
}

// utlsProtectedHosts contains the hosts that should use utls Chrome TLS fingerprint
// to bypass Cloudflare's TLS fingerprinting.
var utlsProtectedHosts = map[string]struct{}{
	"api.anthropic.com": {},
	"chatgpt.com":       {},
}

// fallbackRoundTripper uses utls for protected HTTPS hosts and falls back to
// standard transport for all other requests.
type fallbackRoundTripper struct {
	utls     http.RoundTripper
	fallback http.RoundTripper
}

func (f *fallbackRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "https" {
		if _, ok := utlsProtectedHosts[strings.ToLower(req.URL.Hostname())]; ok {
			return f.utls.RoundTrip(req)
		}
	}
	return f.fallback.RoundTrip(req)
}

// NewUtlsHTTPClient creates an HTTP client using utls Chrome TLS fingerprint.
// Use this for provider requests that need a Chrome-like TLS fingerprint.
// Falls back to standard transport for non-HTTPS requests.
func NewUtlsHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) *http.Client {
	var proxyURL string
	if auth != nil {
		proxyURL = strings.TrimSpace(auth.ProxyURL)
	}
	if proxyURL == "" && cfg != nil {
		proxyURL = strings.TrimSpace(cfg.ProxyURL)
	}

	var ctxRoundTripper http.RoundTripper
	if ctx != nil {
		ctxRoundTripper, _ = ctx.Value("cliproxy.roundtripper").(http.RoundTripper)
	}

	var utlsRT http.RoundTripper = newUtlsRoundTripper(proxyURL)
	var standardTransport http.RoundTripper = http.DefaultTransport
	if proxyURL != "" {
		if transport := buildProxyTransport(proxyURL); transport != nil {
			standardTransport = transport
		}
	} else if ctxRoundTripper != nil {
		utlsRT = ctxRoundTripper
		standardTransport = ctxRoundTripper
	}

	client := &http.Client{
		Transport: &fallbackRoundTripper{
			utls:     utlsRT,
			fallback: standardTransport,
		},
	}
	if timeout > 0 {
		client.Timeout = timeout
	}
	return client
}
