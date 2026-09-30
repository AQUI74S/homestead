package enablebanking

import (
	"context"
	"net"
	"net/http"
	"strings"
)

// PSU describes the user (payment service user) while they are present. Sending these
// headers marks a request as user-initiated; banks do not count such requests
// towards the PSD2 limit for unattended access (usually 4 per day and account).
type PSU struct {
	IPAddress      string
	UserAgent      string
	Referer        string
	Accept         string
	AcceptCharset  string
	AcceptEncoding string
	AcceptLanguage string
}

type psuKey struct{}

// WithPSU marks all bank requests made with the returned context as user-initiated.
func WithPSU(ctx context.Context, p PSU) context.Context {
	if p.IPAddress == "" {
		return ctx
	}
	return context.WithValue(ctx, psuKey{}, p)
}

// WithoutPSU removes the user context (fallback when a bank rejects the headers).
func WithoutPSU(ctx context.Context) context.Context {
	return context.WithValue(ctx, psuKey{}, PSU{})
}

// PSUFrom returns the user context, if any.
func PSUFrom(ctx context.Context) (PSU, bool) {
	p, ok := ctx.Value(psuKey{}).(PSU)
	return p, ok && p.IPAddress != ""
}

// PSUFromRequest builds the user context from an incoming browser request. Behind a
// reverse proxy the client address comes from X-Forwarded-For.
func PSUFromRequest(r *http.Request) PSU {
	ip := ""
	if f := r.Header.Get("X-Forwarded-For"); f != "" {
		ip = strings.TrimSpace(strings.Split(f, ",")[0])
	} else if x := r.Header.Get("X-Real-Ip"); x != "" {
		ip = strings.TrimSpace(x)
	} else if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = h
	}
	return PSU{
		IPAddress:      ip,
		UserAgent:      r.UserAgent(),
		Referer:        r.Referer(),
		Accept:         r.Header.Get("Accept"),
		AcceptCharset:  r.Header.Get("Accept-Charset"),
		AcceptEncoding: r.Header.Get("Accept-Encoding"),
		AcceptLanguage: r.Header.Get("Accept-Language"),
	}
}

func (p PSU) headers() map[string]string {
	h := map[string]string{
		"Psu-Ip-Address":      p.IPAddress,
		"Psu-User-Agent":      p.UserAgent,
		"Psu-Referer":         p.Referer,
		"Psu-Accept":          p.Accept,
		"Psu-Accept-Charset":  p.AcceptCharset,
		"Psu-Accept-Encoding": p.AcceptEncoding,
		"Psu-Accept-Language": p.AcceptLanguage,
	}
	for k, v := range h {
		if v == "" {
			delete(h, k)
		}
	}
	return h
}
