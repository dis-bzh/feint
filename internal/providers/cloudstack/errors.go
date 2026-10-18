package cloudstack

import (
	"net/http"
	"strings"

	"github.com/stephrobert/feint/internal/core/emulator"
)

// apiError is a CloudStack error: an HTTP status and the same number as the
// body's errorcode. cserrorcode is CloudStack's own catalogue of causes. Every
// number and every sentence below was read off a CloudStack 4.23.0.0 management
// server (the official simulator image) on 2026-10-18 rather than written from
// memory; docs/cloudstack.md says how.
type apiError struct {
	status int
	code   int
	cs     int
	text   string
}

// commandMark stands for the name of the command inside an error sentence, which
// the management server writes in lower case ("... API command stopvirtualmachine
// due to ..."). The handler that builds an error does not know the name of the
// command it serves; serve fills it in.
const commandMark = "{command}"

// invalid is the answer to a parameter that is missing.
func invalid(text string) *apiError {
	return &apiError{status: 431, code: 431, cs: 9999, text: text}
}

// missing is the answer to a required parameter that is absent.
func missing(param string) *apiError {
	return invalid("Unable to execute API command " + commandMark + " due to missing parameter " + param)
}

// unknownEntity is the answer to an id that resolves to nothing. The sentence is
// the management server's own, and clients match on it.
func unknownEntity(param, value string) *apiError {
	return invalid("Unable to execute API command " + commandMark + " due to invalid value. Invalid parameter " + param +
		" value=" + value + " due to incorrect long value format, or entity does not exist or due to incorrect parameter annotation for the field in api cmd class.")
}

func unauthenticated() *apiError {
	return &apiError{status: 401, code: 401, text: "unable to verify user credentials"}
}

// writeError writes the error envelope under key.
func writeError(w http.ResponseWriter, key string, e *apiError) {
	body := map[string]any{
		"uuidList":  []any{},
		"errorcode": e.code,
		"errortext": strings.ReplaceAll(e.text, commandMark, strings.TrimSuffix(key, "response")),
	}
	if e.cs != 0 {
		body["cserrorcode"] = e.cs
	}
	emulator.WriteJSON(w, e.status, map[string]any{key: body})
}

// responseKey is the envelope key CloudStack answers a command under: the
// command name, lowercased, plus "response".
func responseKey(command string) string { return strings.ToLower(command) + "response" }
