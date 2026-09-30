package httpapi

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/popiposter/xkeen-control/internal/panellistener"
)

// The accepted socket is the authority. Proxy headers and persisted listener
// policy cannot substitute for it (including on health and embedded assets).
func requestAuthorityAllowed(r *http.Request) bool {
	local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || local == nil {
		return false
	}
	address, err := panellistener.ParseAddress(local.String())
	if err != nil {
		return false
	}
	host := r.Host
	if host == "" || strings.TrimSpace(host) != host || strings.ContainsAny(host, "%/\\@?#") {
		return false
	}
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		// An omitted port means HTTP port 80, including bracketed IPv6.
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			name = host[1 : len(host)-1]
		} else if !strings.ContainsAny(host, ":[]") {
			name = host
		} else {
			return false
		}
		port = "80"
	}
	if port == "" {
		return false
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber != address.Port {
		return false
	}
	localIP := net.ParseIP(address.Host)
	if strings.EqualFold(name, "localhost") {
		return !strings.HasPrefix(host, "[") && localIP.IsLoopback()
	}
	ip := net.ParseIP(name)
	if ip == nil || !ip.Equal(localIP) {
		return false
	}
	// IPv6 must be bracketed; IPv4 must not use IPv6 authority syntax.
	return strings.Contains(name, ":") == strings.HasPrefix(host, "[")
}

// This purpose-specific boundary is used only by the three password-bearing
// routes. Unrelated mutation decoders retain their existing contracts.
func decodePasswordObject(w http.ResponseWriter, r *http.Request, limit int64, fields ...string) (map[string]string, bool) {
	invalid := func() (map[string]string, bool) {
		writeError(w, http.StatusBadRequest, "invalid request")
		return nil, false
	}
	media := r.Header.Values("Content-Type")
	if len(media) != 1 || media[0] != "application/json" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return invalid()
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return invalid()
	}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	value := make(map[string]string, len(fields))
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] {
			return invalid()
		}
		if _, duplicate := value[key]; duplicate {
			return invalid()
		}
		token, err = d.Token()
		text, ok := token.(string)
		if err != nil || !ok {
			return invalid()
		}
		value[key] = text
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('}') || len(value) != len(fields) {
		return invalid()
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return invalid()
	}
	return value, true
}
