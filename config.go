package shopifysso

import (
	"errors"
	"net"
	"net/url"
)

// Config selects the trusted Shopify store, public OAuth client and host collection.
// Configuration is supplied by the host; this package does not read environment variables.
type Config struct {
	StoreURL          string
	RedirectURL       string // Full callback URL chosen and served by the host frontend.
	ClientID          string
	AuthCollection    string // Existing auth collection name or ID. Defaults to users.
	AllowLoopbackHTTP bool   // Only for the local mock and automated tests.
}

// The trusted store URL is operator configuration, never a request parameter.
func checkURL(raw string, allowHTTP, originOnly bool) error {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid configured URL")
	}
	if originOnly && u.Path != "" {
		return errors.New("configure an origin without a path or trailing slash")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if allowHTTP && u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())) {
		return nil
	}
	return errors.New("HTTPS required; HTTP is allowed only on explicit loopback development URLs")
}
