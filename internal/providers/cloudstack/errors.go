package cloudstack

import (
	"net"
	"net/http"
	"strings"

	"github.com/stephrobert/feint/internal/core/emulator"
)

// apiError is a CloudStack error: an HTTP status and the same number as the
// body's errorcode. cserrorcode is CloudStack's own catalogue of causes, and
// minting numbers from it would be inventing a format; 4350 is what the
// management server answers to a parameter it cannot resolve, and 9999 is its
// generic one, so those are the only two used.
type apiError struct {
	status int
	code   int
	cs     int
	text   string
}

// invalid is the answer to a parameter that is missing or that names nothing.
func invalid(text string) *apiError {
	return &apiError{status: 431, code: 431, cs: 9999, text: text}
}

// unknownEntity is the answer to an id that resolves to nothing. The wording is
// CloudStack's own, which clients match on.
func unknownEntity(param, value string) *apiError {
	return &apiError{status: 431, code: 431, cs: 4350, text: "Invalid parameter " + param + " value=" + value +
		" due to incorrect long value format, or entity does not exist or due to incorrect parameter annotation for the field in api cmd class."}
}

func unauthenticated() *apiError {
	return &apiError{status: 401, code: 401, text: "unable to verify user credentials and/or request signature"}
}

// writeError writes the error envelope under key.
func writeError(w http.ResponseWriter, key string, e *apiError) {
	body := map[string]any{
		"uuidList":  []any{},
		"errorcode": e.code,
		"errortext": e.text,
	}
	if e.cs != 0 {
		body["cserrorcode"] = e.cs
	}
	emulator.WriteJSON(w, e.status, map[string]any{key: body})
}

// responseKey is the envelope key CloudStack answers a command under: the
// command name, lowercased, plus "response".
func responseKey(command string) string { return strings.ToLower(command) + "response" }

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
