package cloudstack

import (
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/stephrobert/feint/internal/core/emulator"
)

// param is one parameter of a command, in the words listApis uses for it.
type param struct {
	name     string
	typ      string
	required bool
	desc     string
}

// command is one served CloudStack command. The table in commands() is the one
// place the API surface is written down: Routes mounts it, serve checks the
// required parameters against it, and listApis describes it, so a command
// cannot be mounted without being described or described without being
// mounted.
type command struct {
	name   string
	desc   string
	params []param
	// fields are the names of the response, for listApis. cmk uses them to
	// complete and to filter output; a client that does not ask never sees them.
	fields []string
	// run answers a synchronous command. Exactly one of run and job is set.
	run func(p *Pack, a url.Values) (map[string]any, *apiError)
	// job does the work of an asynchronous command at once and reports what the
	// job settled on; serve wraps it in a job the client polls.
	job func(p *Pack, a url.Values) (outcome, *apiError)
}

// outcome is what an asynchronous command did: the immediate answer (the id of
// what is being created, so a client can already name it) and what the job will
// report.
type outcome struct {
	// ack is what the command answers beyond the jobid. Deploy answers the id of
	// the machine it is creating; start, stop and destroy answer the jobid alone.
	ack map[string]any
	// result is what the job settles on when it succeeds.
	result map[string]any
	// fail is how the job ends when it does not. The command itself was accepted
	// (HTTP 200, a jobid): the refusal travels in the job, which is where a
	// client that only checks the HTTP status of the command will not look.
	fail *jobFailure
	// cmd is the upstream command class, which queryAsyncJobResult reports.
	cmd string
	// instance is the machine the job acts on, reported as jobinstanceid.
	instance string
}

// jobFailure is a job that ended in error: jobstatus 2, the code in
// jobresultcode and in the result.
type jobFailure struct {
	code int
	text string
}

// commands is the served surface.
func commands() []command {
	id := param{"id", "uuid", false, "the ID of the resource"}
	name := param{"name", "string", false, "the name of the resource"}
	keyword := param{"keyword", "string", false, "list by keyword"}
	listall := param{"listall", "boolean", false, "list resources by account"}
	zone := param{"zoneid", "uuid", false, "the ID of the availability zone"}
	cmds := []command{
		{
			name: "listApis", desc: "lists all available apis on the server, provided by the Api Discovery plugin",
			params: []param{{"name", "string", false, "API name"}},
			fields: []string{"name", "description", "isasync", "related", "params", "response"},
			run:    (*Pack).listAPIs,
		},
		{
			name: "listZones", desc: "Lists zones",
			params: []param{id, name, keyword, {"available", "boolean", false, "true if you want to retrieve all available zones"}},
			fields: []string{"id", "name", "networktype", "allocationstate", "securitygroupsenabled", "localstorageenabled"},
			run:    (*Pack).listZones,
		},
		{
			name: "listServiceOfferings", desc: "Lists all available service offerings.",
			params: []param{id, name, keyword, listall},
			fields: []string{"id", "name", "displaytext", "cpunumber", "cpuspeed", "memory"},
			run:    (*Pack).listServiceOfferings,
		},
		{
			name: "listTemplates", desc: "List all public, private, and privileged templates.",
			params: []param{{"templatefilter", "string", true, "possible values are \"featured\", \"self\", \"selfexecutable\", \"sharedexecutable\", \"executable\", and \"community\""}, id, name, keyword, zone, listall},
			fields: []string{"id", "name", "displaytext", "ostypename", "isready", "format", "hypervisor", "zoneid"},
			run:    (*Pack).listTemplates,
		},
		{
			name: "listNetworks", desc: "Lists all available networks.",
			params: []param{id, name, keyword, zone, listall},
			fields: []string{"id", "name", "displaytext", "type", "traffictype", "zoneid", "cidr", "gateway"},
			run:    (*Pack).listNetworks,
		},
		{
			name: "deployVirtualMachine", desc: "Creates and automatically starts a virtual machine based on a service offering, disk offering, and template.",
			params: []param{
				{"serviceofferingid", "uuid", true, "the ID of the service offering for the virtual machine"},
				{"templateid", "uuid", false, "the ID of the template for the virtual machine"},
				{"zoneid", "uuid", true, "availability zone for the virtual machine"},
				name,
				{"displayname", "string", false, "an optional user generated name for the virtual machine"},
				{"networkids", "list", false, "list of network ids used by virtual machine"},
				{"userdata", "string", false, "an optional binary data that can be sent to the virtual machine upon a successful deployment"},
				{"startvm", "boolean", false, "true if start vm after creating; defaulted to true if not specified"},
			},
			fields: vmFields,
			job:    (*Pack).deployVM,
		},
		{
			name: "listVirtualMachines", desc: "List the virtual machines owned by the account.",
			params: []param{id, name, keyword, zone, listall, {"state", "string", false, "state of the virtual machine"}},
			fields: vmFields,
			run:    (*Pack).listVMs,
		},
		{
			name: "startVirtualMachine", desc: "Starts a virtual machine.",
			params: []param{{"id", "uuid", true, "The ID of the virtual machine"}},
			fields: vmFields,
			job:    (*Pack).startVM,
		},
		{
			name: "stopVirtualMachine", desc: "Stops a virtual machine.",
			params: []param{{"id", "uuid", true, "The ID of the virtual machine"}, {"forced", "boolean", false, "Force stop the VM"}},
			fields: vmFields,
			job:    (*Pack).stopVM,
		},
		{
			name: "destroyVirtualMachine", desc: "Destroys a virtual machine.",
			params: []param{{"id", "uuid", true, "The ID of the virtual machine"}, {"expunge", "boolean", false, "If true is passed, the vm is expunged immediately. False by default."}},
			fields: vmFields,
			job:    (*Pack).destroyVM,
		},
		{
			name: "listVolumes", desc: "Lists all volumes.",
			params: []param{id, name, keyword, listall, zone, {"virtualmachineid", "uuid", false, "the ID of the virtual machine"}, {"type", "string", false, "the type of disk volume"}},
			fields: []string{"id", "name", "type", "size", "state", "virtualmachineid", "vmstate", "zoneid", "created"},
			run:    (*Pack).listVolumes,
		},
		{
			name: "queryAsyncJobResult", desc: "Retrieves the current status of asynchronous job.",
			params: []param{{"jobid", "uuid", false, "the ID of the asychronous job"}},
			fields: []string{"jobid", "jobstatus", "jobresultcode", "jobresulttype", "jobresult", "cmd", "created"},
			run:    (*Pack).queryAsyncJobResult,
		},
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].name < cmds[j].name })
	return cmds
}

// serve is the handler of one command: the parts every command shares and none
// of them should repeat.
func (p *Pack) serve(c command) http.HandlerFunc {
	key := responseKey(c.name)
	return func(w http.ResponseWriter, r *http.Request) {
		// Query and form body merged, which is how CloudStack reads them.
		if err := r.ParseForm(); err != nil {
			writeError(w, key, invalid("Unable to read the request: "+err.Error()))
			return
		}
		a := r.Form
		// The envelope's own parameters, and every one this command declares:
		// read by this function, so the unread-field report does not accuse the
		// handler of ignoring what the dispatcher consumed.
		read := []string{"command", "response", "apikey", "signature", "signatureversion", "expires"}
		for _, pa := range c.params {
			read = append(read, pa.name)
		}
		emulator.MarkRead(r, read...)

		// Present, never verified. See docs/limits.md. The envelope's own names
		// are matched without regard to case, as the management server does:
		// cmk sends apiKey, go-cloudstack sends apikey.
		if envelope(a, "apikey") == "" || envelope(a, "signature") == "" {
			writeError(w, key, unauthenticated())
			return
		}
		for _, pa := range c.params {
			if pa.required && a.Get(pa.name) == "" {
				writeError(w, key, missing(pa.name))
				return
			}
		}

		if c.job != nil {
			out, err := c.job(p, a)
			if err != nil {
				writeError(w, key, err)
				return
			}
			body := map[string]any{"jobid": p.newJob(out)}
			for k, v := range out.ack {
				body[k] = v
			}
			emulator.WriteJSON(w, http.StatusOK, map[string]any{key: body})
			return
		}
		body, err := c.run(p, a)
		if err != nil {
			writeError(w, key, err)
			return
		}
		emulator.WriteJSON(w, http.StatusOK, map[string]any{key: body})
	}
}

// listAPIs describes the served surface in listApis's own shape.
//
// This is what makes cmk usable: on `sync` it asks for this list and builds its
// command set out of it, so an emulator that did not answer it would be
// unreachable by the client most people type by hand.
func (p *Pack) listAPIs(a url.Values) (map[string]any, *apiError) {
	want := a.Get("name")
	var apis []any
	for _, c := range commands() {
		if want != "" && c.name != want {
			continue
		}
		params := make([]any, 0, len(c.params))
		for _, pa := range c.params {
			params = append(params, map[string]any{
				"name": pa.name, "description": pa.desc, "required": pa.required,
				"length": 255, "type": pa.typ, "related": "", "since": "3.0.0",
			})
		}
		fields := make([]any, 0, len(c.fields))
		for _, f := range c.fields {
			fields = append(fields, map[string]any{"name": f, "description": f, "type": "string"})
		}
		apis = append(apis, map[string]any{
			"name": c.name, "description": c.desc, "isasync": c.job != nil,
			"related": "", "since": "3.0.0", "params": params, "response": fields,
		})
	}
	if len(apis) == 0 {
		return map[string]any{}, nil
	}
	return map[string]any{"count": len(apis), "api": apis}, nil
}

// envelope reads one of the request's own parameters (apikey, signature, ...)
// whatever case the client spelled it in. The command's parameters are not read
// this way: they are matched exactly, as their names are listed by listApis.
func envelope(a url.Values, name string) string {
	for k, v := range a {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}
