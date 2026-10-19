package cloudstack

import "net/http"

// What an injected failure looks like in CloudStack's dialect.
//
// The core decides when a call fails and with which status; this file decides
// what the client decodes. CloudStack answers every failure in the response
// envelope of the command that failed: errorcode and errortext under the same
// HTTP status. cs and go-cloudstack both raise on errorcode, so that is the
// field an injected fault has to carry. 429 is listed although a stock
// management server does not throttle: an API gateway in front of one does, and
// it is the status a retry policy is worth proving against.

// FaultStatuses implements emulator.Faulter.
func (p *Pack) FaultStatuses() []int {
	return []int{
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
	}
}

// WriteFault implements emulator.Faulter.
func (p *Pack) WriteFault(w http.ResponseWriter, r *http.Request, status int) {
	// "errorresponse" is the key the server answers under when no command was
	// recognised, which is the only name a failure has without one.
	key := "errorresponse"
	if command := commandOf(r); command != "" {
		key = responseKey(command)
	}
	writeError(w, key, &apiError{
		status: status,
		code:   status,
		text:   "feint answered " + http.StatusText(status) + " because a fault rule is armed for this operation; GET /_feint/faults lists them",
	})
}

// commandOf reads the command a request names, from its query or its form body.
func commandOf(r *http.Request) string {
	_ = r.ParseForm()
	return r.Form.Get("command")
}
