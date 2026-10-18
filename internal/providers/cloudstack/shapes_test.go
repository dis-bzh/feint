package cloudstack_test

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
)

// The fixture is the field set of every kind of answer, read off a CloudStack
// 4.23.0.0 management server. This test plays the same scenario against the pack
// and requires the same fields, in both directions.
//
// Both directions, because they fail differently. A field the server answers and
// the pack omits is a client that reads it and gets nothing (cs_instance reads
// instancename and hypervisor, go-cloudstack decodes into typed structs). A field
// the pack answers and the server does not is an invention, which this project
// calls an invented format: nothing downstream can tell it from a real one.
type shapeFixture struct {
	Source   string `json:"source"`
	Recorded string `json:"recorded"`
	Shapes   map[string]struct {
		What  string   `json:"what"`
		Paths []string `json:"paths"`
	} `json:"shapes"`
}

// paths lists every field path of a value, lists flattened under "[]". The free
// form "details" map is one opaque path: its keys are data, not shape.
func paths(v any, prefix string, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			out[p] = true
			if k == "details" {
				continue
			}
			paths(child, p, out)
		}
	case []any:
		for _, child := range x {
			paths(child, prefix+"[]", out)
		}
	}
}

func shapeOf(body map[string]any) map[string]bool {
	out := map[string]bool{}
	paths(body, "", out)
	return out
}

func TestEveryObjectCarriesExactlyTheFieldsTheSimulatorAnswers(t *testing.T) {
	raw, err := os.ReadFile("testdata/simulator-4.23.0.0-shapes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fix shapeFixture
	if err := json.Unmarshal(raw, &fix); err != nil {
		t.Fatal(err)
	}

	c := newClient(t)
	// raw answers: the whole envelope, because the envelope is part of the shape.
	whole := func(v map[string][]string) map[string]any {
		t.Helper()
		return c.wholeAnswer("GET", signedMap(v))
	}
	first := func(answer map[string]any, listKey string) map[string]any {
		return answer[firstKey(answer)].(map[string]any)[listKey].([]any)[0].(map[string]any)
	}
	deploy := func(kv ...string) (string, map[string]any) {
		t.Helper()
		args := map[string][]string{"command": {"deployVirtualMachine"}, "zoneid": {zone}, "templateid": {template}, "serviceofferingid": {small}}
		for i := 0; i < len(kv); i += 2 {
			args[kv[i]] = []string{kv[i+1]}
		}
		ack := whole(args)
		inner := ack[firstKey(ack)].(map[string]any)
		return inner["id"].(string), ack
	}
	cmd := func(command, id string, extra ...string) map[string]any {
		args := map[string][]string{"command": {command}, "id": {id}}
		for i := 0; i < len(extra); i += 2 {
			args[extra[i]] = []string{extra[i+1]}
		}
		return whole(args)
	}
	jobOf := func(ack map[string]any) map[string]any {
		return whole(map[string][]string{"command": {"queryAsyncJobResult"}, "jobid": {ack[firstKey(ack)].(map[string]any)["jobid"].(string)}})
	}
	listVM := func(id string) map[string]any {
		return first(whole(map[string][]string{"command": {"listVirtualMachines"}, "id": {id}}), "virtualmachine")
	}

	got := map[string]map[string]bool{}
	got["zone"] = shapeOf(first(whole(map[string][]string{"command": {"listZones"}}), "zone"))
	got["serviceoffering"] = shapeOf(first(whole(map[string][]string{"command": {"listServiceOfferings"}}), "serviceoffering"))
	got["template"] = shapeOf(first(whole(map[string][]string{"command": {"listTemplates"}, "templatefilter": {"executable"}}), "template"))
	got["network"] = shapeOf(first(whole(map[string][]string{"command": {"listNetworks"}}), "network"))
	got["answer.list_empty"] = shapeOf(whole(map[string][]string{"command": {"listVirtualMachines"}}))

	id, ack := deploy("name", "w")
	got["answer.deploy"] = shapeOf(ack)
	got["job.deploy"] = shapeOf(jobOf(ack))
	got["vm.running"] = shapeOf(listVM(id))
	volumeOf := func(vm string) map[string]any {
		return first(whole(map[string][]string{"command": {"listVolumes"}, "virtualmachineid": {vm}}), "volume")
	}
	got["volume.running"] = shapeOf(volumeOf(id))

	stopAck := cmd("stopVirtualMachine", id)
	got["answer.stop"] = shapeOf(stopAck)
	jobOf(stopAck)
	got["vm.stopped"] = shapeOf(listVM(id))

	jobOf(cmd("startVirtualMachine", id))                              // stopped, so this one runs it again
	got["job.failed"] = shapeOf(jobOf(cmd("startVirtualMachine", id))) // running, so this one fails

	jobOf(cmd("destroyVirtualMachine", id))
	got["vm.destroyed"] = shapeOf(listVM(id))
	got["job.expunge"] = shapeOf(jobOf(cmd("destroyVirtualMachine", id, "expunge", "true")))

	idle, _ := deploy("startvm", "false")
	got["vm.stopped_never_run"] = shapeOf(listVM(idle))
	got["volume.never_run"] = shapeOf(volumeOf(idle))
	jobOf(cmd("destroyVirtualMachine", idle))
	got["vm.destroyed_never_run"] = shapeOf(listVM(idle))

	_, body := c.callWhole("GET", signedMap(map[string][]string{"command": {"stopVirtualMachine"}, "id": {"nope"}}))
	got["error.entity"] = shapeOf(body)
	_, body = c.callWhole("GET", map[string][]string{"command": {"listZones"}, "response": {"json"}})
	got["error.unauthenticated"] = shapeOf(body)
	_, body = c.callWhole("GET", signedMap(map[string][]string{"command": {"createUniverse"}}))
	got["error.unknown_command"] = shapeOf(body)

	names := make([]string, 0, len(fix.Shapes))
	for name := range fix.Shapes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		want := map[string]bool{}
		for _, p := range fix.Shapes[name].Paths {
			want[p] = true
		}
		have, ok := got[name]
		if !ok {
			t.Errorf("%s: the scenario produced no such answer, so nothing was compared (%s)", name, fix.Shapes[name].What)
			continue
		}
		var omitted, invented []string
		for p := range want {
			if !have[p] {
				omitted = append(omitted, p)
			}
		}
		for p := range have {
			if !want[p] {
				invented = append(invented, p)
			}
		}
		sort.Strings(omitted)
		sort.Strings(invented)
		if len(omitted) > 0 {
			t.Errorf("%s (%s): %d field(s) the server answers and this pack omits: %s", name, fix.Shapes[name].What, len(omitted), strings.Join(omitted, ", "))
		}
		if len(invented) > 0 {
			t.Errorf("%s (%s): %d field(s) this pack answers and the server does not: %s", name, fix.Shapes[name].What, len(invented), strings.Join(invented, ", "))
		}
	}
}

func firstKey(m map[string]any) string {
	for k := range m {
		return k
	}
	return ""
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
