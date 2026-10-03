package proxy

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http/httptrace"
	"sync"
	"time"
)

type requestTiming struct {
	logger                            *slog.Logger
	start                             time.Time
	requestID                         string
	estimate, rateLimit, route, quota time.Duration
}

func newRequestTiming(ctx context.Context, requestID string, start time.Time) *requestTiming {
	logger := slog.Default()
	if !logger.Enabled(ctx, slog.LevelDebug) {
		return nil
	}
	return &requestTiming{logger: logger, start: start, requestID: requestID}
}

func (t *requestTiming) begin() time.Time {
	if t == nil {
		return time.Time{}
	}
	return time.Now()
}

func (t *requestTiming) finish(stage string, start time.Time) {
	if t == nil {
		return
	}
	duration := time.Since(start)
	switch stage {
	case "estimate":
		t.estimate += duration
	case "rate_limit":
		t.rateLimit += duration
	case "route":
		t.route += duration
	case "quota":
		t.quota += duration
	}
}

type upstreamTiming struct {
	request                                           *requestTiming
	channelID, attempt                                int
	start                                             time.Time
	mu                                                sync.Mutex
	dnsStart, tlsStart                                time.Time
	tcpStarts                                         map[string]time.Time
	dns, tcp, tls                                     time.Duration
	dnsSeen, tcpSeen, tlsSeen, connectionSeen, reused bool
	headers                                           time.Duration
	headersSeen                                       bool
}

func (t *requestTiming) trace(ctx context.Context, channelID, attempt int) (context.Context, *upstreamTiming) {
	if t == nil {
		return ctx, nil
	}
	u := &upstreamTiming{request: t, channelID: channelID, attempt: attempt, start: time.Now(), tcpStarts: make(map[string]time.Time)}
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { u.mu.Lock(); defer u.mu.Unlock(); u.dnsStart = time.Now() },
		DNSDone: func(httptrace.DNSDoneInfo) {
			u.mu.Lock()
			defer u.mu.Unlock()
			if !u.dnsStart.IsZero() {
				u.dns += time.Since(u.dnsStart)
				u.dnsSeen = true
			}
		},
		ConnectStart: func(network, addr string) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.tcpStarts[network+"\x00"+addr] = time.Now()
		},
		ConnectDone: func(network, addr string, _ error) {
			u.mu.Lock()
			defer u.mu.Unlock()
			key := network + "\x00" + addr
			if start, ok := u.tcpStarts[key]; ok {
				u.tcp += time.Since(start)
				u.tcpSeen = true
				delete(u.tcpStarts, key)
			}
		},
		TLSHandshakeStart: func() { u.mu.Lock(); defer u.mu.Unlock(); u.tlsStart = time.Now() },
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			u.mu.Lock()
			defer u.mu.Unlock()
			if !u.tlsStart.IsZero() {
				u.tls += time.Since(u.tlsStart)
				u.tlsSeen = true
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			u.mu.Lock()
			defer u.mu.Unlock()
			u.connectionSeen = true
			u.reused = info.Reused
		},
	}
	return httptrace.WithClientTrace(ctx, trace), u
}

func (u *upstreamTiming) responseHeaders() {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.headers = time.Since(u.start)
	u.headersSeen = true
}

func (u *upstreamTiming) emit(ctx context.Context, event string) {
	if u == nil {
		return
	}
	now := time.Now()
	u.mu.Lock()
	defer u.mu.Unlock()
	t := u.request
	attrs := []slog.Attr{
		slog.String("request_id", t.requestID), slog.Int("channel_id", u.channelID), slog.Int("attempt", u.attempt), slog.String("event", event),
		slog.Float64("preflight_estimate_ms", float64(t.estimate)/float64(time.Millisecond)),
		slog.Float64("preflight_rate_limit_ms", float64(t.rateLimit)/float64(time.Millisecond)),
		slog.Float64("preflight_route_ms", float64(t.route)/float64(time.Millisecond)),
		slog.Float64("preflight_quota_ms", float64(t.quota)/float64(time.Millisecond)),
	}
	if u.dnsSeen {
		attrs = append(attrs, slog.Float64("dns_ms", float64(u.dns)/float64(time.Millisecond)))
	}
	if u.tcpSeen {
		attrs = append(attrs, slog.Float64("tcp_ms", float64(u.tcp)/float64(time.Millisecond)))
	}
	if u.tlsSeen {
		attrs = append(attrs, slog.Float64("tls_ms", float64(u.tls)/float64(time.Millisecond)))
	}
	if u.connectionSeen {
		attrs = append(attrs, slog.Bool("connection_reused", u.reused))
	}
	if u.headersSeen {
		attrs = append(attrs, slog.Float64("response_headers_ms", float64(u.headers)/float64(time.Millisecond)))
	}
	if event == "first_data_frame" {
		attrs = append(attrs, slog.Float64("first_data_frame_ms", float64(now.Sub(u.start))/float64(time.Millisecond)), slog.Float64("chat_to_first_data_frame_ms", float64(now.Sub(t.start))/float64(time.Millisecond)))
	}
	t.logger.LogAttrs(ctx, slog.LevelDebug, "proxy_latency", attrs...)
}
