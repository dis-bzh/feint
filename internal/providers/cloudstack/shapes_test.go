package cloudstack_test

import (
	"net/http"
	"sort"
	"strings"
	"testing"
)

// Every kind of answer the pack gives has the fields the real server gives it, in
// both directions. A field the server always answers and the pack omits is a client
// that reads it and gets nothing (cs_instance reads instancename and hypervisor, and
// go-cloudstack decodes into typed structs). A field the pack answers and the server
// never does is an invention, which this project calls an invented format: nothing
// downstream can tell it from a real one.
//
// The server's side is the recording (recording_test.go), not a second fixture, so
// there is nothing to regenerate in step with it.
type kindOfAnswer struct {
	name string
	what string
	real func(r map[string]recorded) any
}

func body(label string) func(map[string]recorded) any {
	return func(r map[string]recorded) any { return r[label].Body }
}

// first is the first object of a list answer, under the item key.
func first(label, key string) func(map[string]recorded) any {
	return func(r map[string]recorded) any {
		inner := onlyValue(r[label].Body)
		return inner[key].([]any)[0]
	}
}

func onlyValue(m map[string]any) map[string]any {
	for _, v := range m {
		return v.(map[string]any)
	}
	return nil
}

// jobMachine is the machine a finished job reports, without the job's own
// marker, which a list does not carry.
func jobMachine(label string) func(map[string]recorded) any {
	return func(r map[string]recorded) any {
		vm := onlyValue(r[label].Body)["jobresult"].(map[string]any)["virtualmachine"].(map[string]any)
		out := map[string]any{}
		for k, v := range vm {
			if k != "jobid" && k != "jobstatus" {
				out[k] = v
			}
		}
		return out
	}
}

var kindsOfAnswer = []kindOfAnswer{
	{"zone", "a zone, as listZones answers it", first("zones", "zone")},
	{"serviceoffering", "a service offering", first("offerings", "serviceoffering")},
	{"template", "a template", first("templates", "template")},
	{"network", "a network", first("networks", "network")},
	{"vm.running", "a running machine in a list", first("vms-by-name", "virtualmachine")},
	// A list does not give the history of a machine that ran and was stopped, so it is
	// read off the stop job, which carries the same view plus the job's marker.
	{"vm.stopped", "a machine stopped after it ran", jobMachine("stop-job")},
	{"vm.stopped_never_run", "a machine deployed with startvm=false and never started", first("vms2-state", "virtualmachine")},
	{"vm.destroyed", "a machine destroyed after it ran", first("vms-destroyed", "virtualmachine")},
	{"vm.destroyed_never_run", "a machine destroyed without ever having run", first("vms2-destroyed", "virtualmachine")},
	{"volume.running", "the root volume of a machine that has run", first("volumes-running", "volume")},
	{"volume.never_run", "the root volume of a machine that never ran", first("volumes-never-run", "volume")},
	{"answer.list_empty", "a list with nothing in it", body("vms-empty")},
	{"answer.page_past", "a page past the end: the count survives", body("zones-page-past")},
	{"answer.deploy", "the immediate answer to deployVirtualMachine", body("deploy")},
	{"answer.stop", "the immediate answer to stop, start and destroy: the jobid alone", body("stop")},
	{"answer.apis", "listApis for one command", body("apis-zones")},
	{"job.deploy", "queryAsyncJobResult for a finished deploy", body("deploy-job")},
	{"job.expunge", "queryAsyncJobResult for a finished expunge", body("expunge-job")},
	{"job.failed", "queryAsyncJobResult for a job that failed", body("start-running-job")},
	{"error.entity", "an id that names nothing", body("err-unknown-id")},
	{"error.pagination", "a page size without a page", body("pagesize-only")},
	{"error.apis_unknown", "listApis for a command that does not exist", body("apis-unknown")},
	{"error.unauthenticated", "a request without credentials", body("no-auth")},
	{"error.unknown_command", "a command that does not exist", body("err-unknown-command")},
}

// play drives the pack through every history a machine has, and returns the
// answer of each kind. It is also what the contract test runs against a server
// that validates its own answers.
func play(t *testing.T, c *client) map[string]any {
	t.Helper()
	got := map[string]any{}
	whole := func(command string, kv ...string) map[string]any {
		t.Helper()
		args := map[string][]string{"command": {command}}
		for i := 0; i < len(kv); i += 2 {
			args[kv[i]] = []string{kv[i+1]}
		}
		return c.wholeAnswer("GET", signedMap(args))
	}
	item := func(answer map[string]any, key string) any {
		return onlyValue(answer)[key].([]any)[0]
	}
	job := func(ack map[string]any) map[string]any {
		return whole("queryAsyncJobResult", "jobid", onlyValue(ack)["jobid"].(string))
	}
	deploy := func(kv ...string) (string, map[string]any) {
		ack := whole("deployVirtualMachine", append([]string{"zoneid", zone, "templateid", template, "serviceofferingid", small}, kv...)...)
		return onlyValue(ack)["id"].(string), ack
	}
	machine := func(id string) any { return item(whole("listVirtualMachines", "id", id), "virtualmachine") }
	volume := func(id string) any { return item(whole("listVolumes", "virtualmachineid", id), "volume") }

	got["zone"] = item(whole("listZones"), "zone")
	got["serviceoffering"] = item(whole("listServiceOfferings"), "serviceoffering")
	got["template"] = item(whole("listTemplates", "templatefilter", "executable"), "template")
	got["network"] = item(whole("listNetworks"), "network")
	got["answer.list_empty"] = whole("listVirtualMachines", "name", "no-such-machine-name")
	got["answer.page_past"] = whole("listZones", "pagesize", "1", "page", "2")
	got["answer.apis"] = whole("listApis", "name", "listZones")

	id, ack := deploy("name", "w")
	got["answer.deploy"] = ack
	got["job.deploy"] = job(ack)
	got["vm.running"] = machine(id)
	got["volume.running"] = volume(id)

	stop := whole("stopVirtualMachine", "id", id)
	got["answer.stop"] = stop
	job(stop)
	got["vm.stopped"] = machine(id)

	job(whole("startVirtualMachine", "id", id))                     // stopped, so this one runs it again
	got["job.failed"] = job(whole("startVirtualMachine", "id", id)) // running, so this one fails
	job(whole("destroyVirtualMachine", "id", id))
	got["vm.destroyed"] = machine(id)
	got["job.expunge"] = job(whole("destroyVirtualMachine", "id", id, "expunge", "true"))

	idle, _ := deploy("startvm", "false")
	got["vm.stopped_never_run"] = machine(idle)
	got["volume.never_run"] = volume(idle)
	job(whole("destroyVirtualMachine", "id", idle))
	got["vm.destroyed_never_run"] = machine(idle)

	fail := func(kv map[string][]string) map[string]any {
		t.Helper()
		_, b := c.callWhole("GET", kv)
		return b
	}
	got["error.entity"] = fail(signedMap(map[string][]string{"command": {"stopVirtualMachine"}, "id": {"nope"}}))
	got["error.pagination"] = fail(signedMap(map[string][]string{"command": {"listServiceOfferings"}, "pagesize": {"1"}}))
	got["error.apis_unknown"] = fail(signedMap(map[string][]string{"command": {"listApis"}, "name": {"nope"}}))
	got["error.unauthenticated"] = fail(map[string][]string{"command": {"listZones"}, "response": {"json"}})
	got["error.unknown_command"] = fail(signedMap(map[string][]string{"command": {"createUniverse"}}))
	return got
}

func TestEveryObjectCarriesExactlyTheFieldsTheSimulatorAnswers(t *testing.T) {
	recording := recordingByLabel(t)
	got := play(t, newClient(t))

	names := make([]string, 0, len(kindsOfAnswer))
	for _, k := range kindsOfAnswer {
		names = append(names, k.name)
		mine, ok := got[k.name]
		if !ok {
			t.Errorf("%s: the scenario produced no such answer, so nothing was compared (%s)", k.name, k.what)
			continue
		}
		omitted, invented := difference(shapeOf(k.real(recording)), shapeOf(mine))
		if len(omitted) > 0 {
			t.Errorf("%s (%s): %d field(s) the server always answers and this pack omits: %s", k.name, k.what, len(omitted), strings.Join(omitted, ", "))
		}
		if len(invented) > 0 {
			t.Errorf("%s (%s): %d field(s) this pack answers and the server never does: %s", k.name, k.what, len(invented), strings.Join(invented, ", "))
		}
	}
	// And the other way round: an answer the scenario gives and no kind compares is a
	// shape nothing holds.
	sort.Strings(names)
	for name := range got {
		if i := sort.SearchStrings(names, name); i == len(names) || names[i] != name {
			t.Errorf("%s: the scenario produces it and no kind of answer compares it", name)
		}
	}
}

func signedMap(v map[string][]string) map[string][]string {
	out := map[string][]string{"response": {"json"}, "apikey": {"k"}, "signature": {"s"}}
	for k, x := range v {
		out[k] = x
	}
	return out
}

// wholeAnswer is call() keeping the envelope key, which the shape includes.
func (c *client) wholeAnswer(method string, v map[string][]string) map[string]any {
	c.t.Helper()
	status, body := c.callWhole(method, v)
	if status != http.StatusOK {
		c.t.Fatalf("%s %v answered %d: %v", method, v["command"], status, body)
	}
	return body
}
