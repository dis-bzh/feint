// Package cloudstack emulates the Apache CloudStack API.
//
// CloudStack speaks a third kind of dialect, and that is the point of hosting it
// here. There is one URL, /client/api, and the operation is a `command`
// parameter: in the query of a GET, in the form body of a POST. Every answer is
// a JSON object with a single key, "<command>response", and an error is that
// same envelope carrying errorcode and errortext under an HTTP status of the
// same number. Anything that changes something returns at once with a jobid,
// and the client polls queryAsyncJobResult until the job settles.
//
// The pack declares its commands once, in commands.go: the table is the source
// of the routes, of the parameter checks and of the listApis answer, so the
// three cannot drift apart. cloudmonkey (cmk) builds its whole command set out
// of listApis, which is why that answer is generated rather than written.
//
// This is a prototype. What it serves is the shortest path a real client takes
// from nothing to a machine and back: the catalogue reads, deployVirtualMachine,
// start, stop, destroy and the job poll. docs/limits.md says what it does not do.
package cloudstack

import (
	"net/http"
	"time"

	"github.com/stephrobert/feint/internal/core/emulator"
	"github.com/stephrobert/feint/internal/core/resource"
)

// Name is the provider key.
const Name = "cloudstack"

// pathPrefix is the one URL the whole API lives on.
const pathPrefix = "/client/api"

// The resource kinds this pack stores. The catalogue (zones, templates,
// offerings) is a table in the binary and is never stored.
const (
	kindVM  = "virtualmachine"
	kindJob = "asyncjob"
	// kindGone is what an expunged machine leaves: its identifier and its
	// database sequence number, and nothing else. The management server keeps the
	// row, flagged removed, and three of its answers depend on it: a list by that
	// id is empty rather than an error, an action on it is accepted and fails in
	// the job, and the sequence number is what the job's error names.
	kindGone = "expungedvm"
)

// Pack implements emulator.Pack for CloudStack.
type Pack struct {
	env *emulator.Env
	// epoch is when the catalogue was registered: the creation date of the
	// zone's offerings, template and network, fixed so that it does not move
	// between two reads.
	epoch time.Time
}

// New returns a CloudStack pack backed by env.
func New(env *emulator.Env) *Pack { return &Pack{env: env, epoch: env.Now()} }

// Name implements emulator.Pack.
func (p *Pack) Name() string { return Name }

// operation returns the canonical name of a CloudStack command. The upstream
// name is the command itself, as listApis spells it.
func operation(command string) string { return "cs/API." + command }

// tenant is the one tenancy this prototype has: the ROOT domain's admin
// account. CloudStack's domain, account and project model is not emulated.
func tenant() resource.Tenant { return resource.Tenant{Provider: Name} }

// Routes implements emulator.Pack.
//
// Every command is mounted twice, GET and POST, because both are in use:
// go-cloudstack and the Python cs library send GET, cmk sends POST with a form
// body. The two share an operation name, since upstream they are one command.
func (p *Pack) Routes() []emulator.Route {
	cmds := commands()
	routes := make([]emulator.Route, 0, 2*len(cmds))
	for _, c := range cmds {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			routes = append(routes, emulator.Route{
				Method:    method,
				Path:      pathPrefix + "?command=" + c.name,
				Operation: operation(c.name),
				Handler:   p.serve(c),
			})
		}
	}
	return routes
}

// Declined implements emulator.Pack. The prototype has no contract to join
// against yet, so there is no upstream list to subtract from; the declarations
// arrive with the listApis extractor.
func (p *Pack) Declined() []emulator.Decline { return nil }

// Env implements emulator.Pack.
//
// Both spellings are exported because the two families of client read different
// names: the Terraform provider and go-cloudstack read CLOUDSTACK_API_URL,
// CLOUDSTACK_API_KEY and CLOUDSTACK_SECRET_KEY, while the Python cs library
// under ngine_io.cloudstack reads CLOUDSTACK_ENDPOINT, CLOUDSTACK_KEY and
// CLOUDSTACK_SECRET. The keys are public and open nothing: the emulator does
// not verify a signature, but the clients refuse to send a request without one.
func (p *Pack) Env(endpoint string) emulator.Environment {
	api := endpoint + pathPrefix
	return emulator.Environment{
		Vars: map[string]string{
			"CLOUDSTACK_API_URL":    api,
			"CLOUDSTACK_API_KEY":    fakeKey,
			"CLOUDSTACK_SECRET_KEY": fakeSecret,
			"CLOUDSTACK_ENDPOINT":   api,
			"CLOUDSTACK_KEY":        fakeKey,
			"CLOUDSTACK_SECRET":     fakeSecret,
		},
		Note: "cmk keeps its endpoint in its own profile rather than in the environment: " +
			"cmk set url " + api + "; cmk set apikey " + fakeKey + "; cmk set secretkey " + fakeSecret + "; cmk sync",
	}
}

const (
	fakeKey    = "feint-api-key"
	fakeSecret = "feint-secret-key"
)

// Prefixes implements emulator.Unrouted.
func (p *Pack) Prefixes() []string { return []string{pathPrefix} }

// NotFound implements emulator.Unrouted: a command nobody serves is answered in
// CloudStack's own words, which cmk and cs both surface. A request that names no
// command at all is answered 200 with an empty body, as the management server
// does: a client that sent one has a bug, and this is what it will see upstream.
func (p *Pack) NotFound(w http.ResponseWriter, r *http.Request) {
	command := commandOf(r)
	if command == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	writeError(w, "errorresponse", &apiError{status: 432, code: 432, cs: 9999, text: "Unknown API command: " + command})
}
