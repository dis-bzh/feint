package cloudstack_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Everything below was read off a CloudStack 4.23.0.0 management server (the
// official simulator image) before it was written here: the scenario was played
// against that server and against this pack, and every case is one where the
// first version of the pack disagreed with it. A test that merely restates the
// pack proves nothing; these restate the server.

// job returns a finished job's whole answer, success or not.
func (c *client) job(method string, ack map[string]any) map[string]any {
	c.t.Helper()
	jobID, _ := ack["jobid"].(string)
	if jobID == "" {
		c.t.Fatalf("an asynchronous command answered without a jobid: %v", ack)
	}
	return c.ok(method, signed("queryAsyncJobResult", "jobid", jobID))
}

func (c *client) deploy(extra ...string) (id string, ack map[string]any) {
	c.t.Helper()
	kv := append([]string{"zoneid", zone, "templateid", template, "serviceofferingid", small}, extra...)
	ack = c.ok("GET", signed("deployVirtualMachine", kv...))
	id, _ = ack["id"].(string)
	c.job("GET", ack)
	return id, ack
}

// The command is accepted and the job fails: the HTTP status of the command says
// nothing, and a client that only checks it will miss the refusal.
func TestAStateErrorTravelsInTheJobNotInTheStatus(t *testing.T) {
	c := newClient(t)
	id, _ := c.deploy("name", "web")

	ack := c.ok("GET", signed("startVirtualMachine", "id", id))
	if _, hasID := ack["id"]; hasID || len(ack) != 1 {
		t.Errorf("start answers the jobid alone, got %v", ack)
	}
	job := c.job("GET", ack)
	result, _ := job["jobresult"].(map[string]any)
	if job["jobstatus"] != float64(2) || job["jobresultcode"] != float64(431) || result["errorcode"] != float64(431) {
		t.Fatalf("starting a running machine must fail the job with 431: %v", job)
	}
	if want := "The virtual machine " + id + " (web) is already running"; result["errortext"] != want {
		t.Errorf("errortext %q, want %q", result["errortext"], want)
	}
}

func TestStoppingAMachineThatIsNotRunningSucceeds(t *testing.T) {
	c := newClient(t)
	id, _ := c.deploy()
	c.job("GET", c.ok("GET", signed("stopVirtualMachine", "id", id)))

	// In this order and no other: the second case starts from what the first left.
	for _, step := range []struct {
		name, want string
		setup      func()
	}{
		{"stopped", "Stopped", func() {}},
		{"destroyed", "Destroyed", func() { c.job("GET", c.ok("GET", signed("destroyVirtualMachine", "id", id))) }},
	} {
		step.setup()
		job := c.job("GET", c.ok("GET", signed("stopVirtualMachine", "id", id)))
		vm := job["jobresult"].(map[string]any)["virtualmachine"].(map[string]any)
		if job["jobstatus"] != float64(1) || vm["state"] != step.want {
			t.Errorf("stop on a %s machine: status %v, state %v; want success and %s", step.name, job["jobstatus"], vm["state"], step.want)
		}
	}
	// Destroying twice is the same: it answers the machine as it is.
	again := c.job("GET", c.ok("GET", signed("destroyVirtualMachine", "id", id)))
	if again["jobstatus"] != float64(1) {
		t.Errorf("destroy on a destroyed machine: %v", again)
	}
}

func TestStartingADestroyedMachineFailsInTheOrchestrator(t *testing.T) {
	c := newClient(t)
	id, _ := c.deploy()
	c.job("GET", c.ok("GET", signed("destroyVirtualMachine", "id", id)))

	job := c.job("GET", c.ok("GET", signed("startVirtualMachine", "id", id)))
	text, _ := job["jobresult"].(map[string]any)["errortext"].(string)
	if job["jobstatus"] != float64(2) || job["jobresultcode"] != float64(530) ||
		!strings.HasPrefix(text, "Unable to orchestrate the start of VM instance") || !strings.Contains(text, id) {
		t.Errorf("job %v", job)
	}
}

// A machine takes an address the first time it starts and keeps it until it is
// expunged. One that never ran has none. An earlier version had this the other
// way round, having read it off machines that had never started.
func TestAMachineKeepsItsAddressFromItsFirstStartUntilItIsExpunged(t *testing.T) {
	c := newClient(t)
	addr := func(id string) string {
		vm := c.ok("GET", signed("listVirtualMachines", "id", id))["virtualmachine"].([]any)[0].(map[string]any)
		ip, _ := vm["nic"].([]any)[0].(map[string]any)["ipaddress"].(string)
		return ip
	}
	first, _ := c.deploy()
	ip1 := addr(first)
	if ip1 == "" {
		t.Fatal("a running machine has an address")
	}

	// Stopped, it keeps it: the address is not free for anyone else.
	c.job("GET", c.ok("GET", signed("stopVirtualMachine", "id", first)))
	if got := addr(first); got != ip1 {
		t.Errorf("a stopped machine lost its address: %q, had %q", got, ip1)
	}
	second, _ := c.deploy()
	if ip2 := addr(second); ip2 == "" || ip2 == ip1 {
		t.Errorf("a second machine took %q, which the first still holds (%s)", ip2, ip1)
	}
	c.job("GET", c.ok("GET", signed("startVirtualMachine", "id", first)))
	if got := addr(first); got != ip1 {
		t.Errorf("a restarted machine changed address: %q, had %q", got, ip1)
	}

	// One that never ran has none, and takes one at its first start.
	idle, _ := c.deploy("startvm", "false")
	if got := addr(idle); got != "" {
		t.Errorf("a machine that never ran has an address: %q", got)
	}
	c.job("GET", c.ok("GET", signed("startVirtualMachine", "id", idle)))
	if got := addr(idle); got == "" || got == ip1 {
		t.Errorf("the first start must take a free address, got %q", got)
	}

	// Expunged, it gives the address back.
	c.job("GET", c.ok("GET", signed("destroyVirtualMachine", "id", first, "expunge", "true")))
	third, _ := c.deploy()
	if got := addr(third); got != ip1 {
		t.Errorf("the address of an expunged machine was not reused: got %q, want %q", got, ip1)
	}
}

func TestAnExpungedMachineLeavesARowAndNothingElse(t *testing.T) {
	c := newClient(t)
	id, _ := c.deploy()
	job := c.job("GET", c.ok("GET", signed("destroyVirtualMachine", "id", id, "expunge", "true")))
	vm := job["jobresult"].(map[string]any)["virtualmachine"].(map[string]any)
	if _, ok := vm["id"]; ok || vm["deleteprotection"] != false {
		t.Errorf("an expunge answers a machine with nothing in it, got %v", vm)
	}

	// Listing it is empty, not an error: the row exists, flagged removed.
	if got := c.ok("GET", signed("listVirtualMachines", "id", id)); len(got) != 0 {
		t.Errorf("list of an expunged id: %v", got)
	}
	// Acting on it is accepted and the job fails, naming the database number.
	failed := c.job("GET", c.ok("GET", signed("stopVirtualMachine", "id", id)))
	text, _ := failed["jobresult"].(map[string]any)["errortext"].(string)
	if failed["jobstatus"] != float64(2) || failed["jobresultcode"] != float64(431) ||
		!strings.HasPrefix(text, "unable to find a virtual machine with id ") {
		t.Errorf("job %v", failed)
	}
	// An id the cloud never held is another matter: refused at the door.
	status, body := c.call("GET", signed("listVirtualMachines", "id", "nope"))
	if status != 431 || !strings.Contains(body["errortext"].(string), "Invalid parameter id value=nope") {
		t.Errorf("list of an unknown id: %d %v", status, body)
	}
}

func TestADeployWithoutANameIsCalledAfterItsId(t *testing.T) {
	c := newClient(t)
	id, _ := c.deploy()
	vm := c.ok("GET", signed("listVirtualMachines", "id", id))["virtualmachine"].([]any)[0].(map[string]any)
	if vm["name"] != "VM-"+id || vm["displayname"] != "VM-"+id {
		t.Errorf("name %v, displayname %v", vm["name"], vm["displayname"])
	}
	// startvm is read as true when it is not a boolean.
	odd, _ := c.deploy("startvm", "maybe")
	state := c.ok("GET", signed("listVirtualMachines", "id", odd))["virtualmachine"].([]any)[0].(map[string]any)["state"]
	if state != "Running" {
		t.Errorf("startvm=maybe deployed a %v machine", state)
	}
}

func TestTheEnumerationErrorIsTheJavaExceptionsOwn(t *testing.T) {
	c := newClient(t)
	status, body := c.call("GET", signed("listTemplates", "templatefilter", "bogus"))
	want := "No enum constant com.cloud.template.VirtualMachineTemplate.TemplateFilter.bogus"
	if status != 431 || body["errortext"] != want {
		t.Errorf("%d %v", status, body)
	}
	status, body = c.call("GET", signed("deployVirtualMachine", "zoneid", zone, "serviceofferingid", small))
	if status != 530 || body["cserrorcode"] != float64(4250) {
		t.Errorf("deploy with no template: %d %v", status, body)
	}
}

// A request that names no command is answered 200 and nothing: a client that
// sent one has a bug, and this is what it meets upstream.
func TestARequestWithNoCommandIsAnsweredEmpty(t *testing.T) {
	c := newClient(t)
	resp, err := http.Post(c.srv.URL+"/client/api", "application/x-www-form-urlencoded",
		strings.NewReader(url.Values{"response": {"json"}, "apikey": {"k"}, "signature": {"s"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || len(body) != 0 {
		t.Errorf("%d %q", resp.StatusCode, body)
	}
}

// bootmode and boottype are answered by a machine that has not been stopped, and
// are not restored when it starts again.
func TestARestartedMachineAnswersNoBootMode(t *testing.T) {
	c := newClient(t)
	id, _ := c.deploy()
	view := func() map[string]any {
		return c.ok("GET", signed("listVirtualMachines", "id", id))["virtualmachine"].([]any)[0].(map[string]any)
	}
	if v := view(); v["bootmode"] != "legacy" || v["boottype"] != "Bios" {
		t.Fatalf("a freshly deployed machine reports its boot mode: %v %v", v["bootmode"], v["boottype"])
	}
	c.job("GET", c.ok("GET", signed("stopVirtualMachine", "id", id)))
	c.job("GET", c.ok("GET", signed("startVirtualMachine", "id", id)))
	v := view()
	if _, has := v["bootmode"]; has {
		t.Errorf("a restarted machine reports a boot mode: %v", v["bootmode"])
	}
	if v["state"] != "Running" || v["hostid"] == nil {
		t.Errorf("it still runs on a host: %v %v", v["state"], v["hostid"])
	}
}

// A machine has a root volume that follows it: Allocated until the machine first
// runs, Ready after, listed while the machine is, empty once it is expunged.
func TestTheRootVolumeFollowsItsMachine(t *testing.T) {
	c := newClient(t)
	volume := func(vm string) (map[string]any, int) {
		got := c.ok("GET", signed("listVolumes", "virtualmachineid", vm))
		if len(got) == 0 {
			return nil, 0
		}
		return got["volume"].([]any)[0].(map[string]any), int(got["count"].(float64))
	}

	idle, _ := c.deploy("startvm", "false")
	v, _ := volume(idle)
	if v["state"] != "Allocated" || v["hypervisor"] != "None" || v["vmstate"] != "Stopped" {
		t.Errorf("a machine that never ran has an Allocated volume: %v", v)
	}
	c.job("GET", c.ok("GET", signed("startVirtualMachine", "id", idle)))
	v, n := volume(idle)
	if n != 1 || v["state"] != "Ready" || v["type"] != "ROOT" || v["vmstate"] != "Running" || v["size"].(float64) != 2147483648 {
		t.Errorf("after its first start the volume is Ready: %d %v", n, v)
	}
	// It is found by its own id and by its type, and a data disk is nothing.
	byID := c.ok("GET", signed("listVolumes", "id", v["id"].(string)))
	if byID["count"] != float64(1) {
		t.Errorf("list by volume id: %v", byID)
	}
	if got := c.ok("GET", signed("listVolumes", "virtualmachineid", idle, "type", "DATADISK")); len(got) != 0 {
		t.Errorf("a root volume is not a data disk: %v", got)
	}

	c.job("GET", c.ok("GET", signed("destroyVirtualMachine", "id", idle, "expunge", "true")))
	if _, n := volume(idle); n != 0 {
		t.Errorf("an expunged machine keeps no volume, got %d", n)
	}
	// Its own id is held, an unknown one is refused.
	if status, body := c.call("GET", signed("listVolumes", "virtualmachineid", "nope")); status != 431 ||
		!strings.Contains(body["errortext"].(string), "Invalid parameter virtualmachineid value=nope") {
		t.Errorf("unknown machine: %d %v", status, body)
	}
}

// page and pagesize, with the rules the server applies (oracle.py, the page-* steps):
// count is the total the filters matched and not the size of the page, a page past
// the end keeps the count and loses its items, and the two parameters go together.
func TestPagesCountTheTotalAndPageTheItems(t *testing.T) {
	c := newClient(t)
	names := func(v url.Values) (int, []string) {
		body := c.ok("GET", v)
		var got []string
		for _, o := range body["serviceoffering"].([]any) {
			got = append(got, o.(map[string]any)["name"].(string))
		}
		return int(body["count"].(float64)), got
	}
	cases := []struct {
		name  string
		extra []string
		count int
		items []string
	}{
		{"first page", []string{"pagesize", "1", "page", "1"}, 2, []string{"feint.small"}},
		{"second page", []string{"pagesize", "1", "page", "2"}, 2, []string{"feint.medium"}},
		{"everything on one page", []string{"pagesize", "5", "page", "1"}, 2, []string{"feint.small", "feint.medium"}},
		{"a page below the first is the first", []string{"pagesize", "1", "page", "0"}, 2, []string{"feint.small"}},
		{"-1 asks for everything", []string{"pagesize", "-1"}, 2, []string{"feint.small", "feint.medium"}},
		{"the count is the matches, not the page", []string{"pagesize", "1", "page", "1", "keyword", "medium"}, 1, []string{"feint.medium"}},
	}
	for _, tc := range cases {
		count, items := names(signed("listServiceOfferings", tc.extra...))
		if count != tc.count || strings.Join(items, ",") != strings.Join(tc.items, ",") {
			t.Errorf("%s: count %d items %v, want %d %v", tc.name, count, items, tc.count, tc.items)
		}
	}

	// Past the end: the count survives and the items key is gone.
	past := c.ok("GET", signed("listServiceOfferings", "pagesize", "1", "page", "3"))
	if _, has := past["serviceoffering"]; has || past["count"] != float64(2) {
		t.Errorf("a page past the end must keep its count and lose its items: %v", past)
	}

	refused := []struct {
		name   string
		extra  []string
		cs     float64
		errtxt string
	}{
		{"pagesize without page", []string{"pagesize", "1"}, 9999, `"page" parameter is required when "pagesize" is specified`},
		{"page without pagesize", []string{"page", "1"}, 9999, `"pagesize" parameter is required when "page" is specified`},
		{"-1 takes no page", []string{"pagesize", "-1", "page", "1"}, 4350, "Can't specify page parameter when pagesize is -1 (Unlimited)"},
		{"more than the server's bound", []string{"pagesize", "1000", "page", "1"}, 4350, "Page size can't exceed max allowed page size value: 500"},
		{"a page that is not a number", []string{"pagesize", "1", "page", "abc"}, 9999, "due to invalid value abc for parameter page"},
		{"a page size that is not a number", []string{"pagesize", "abc", "page", "1"}, 9999, "due to invalid value abc for parameter pagesize"},
	}
	for _, tc := range refused {
		status, body := c.call("GET", signed("listServiceOfferings", tc.extra...))
		if status != 431 || body["cserrorcode"] != tc.cs || !strings.Contains(body["errortext"].(string), tc.errtxt) {
			t.Errorf("%s: %d %v", tc.name, status, body)
		}
	}
	// page is read before pagesize, so a request wrong in both is refused for the page.
	_, both := c.call("GET", signed("listServiceOfferings", "page", "abc", "pagesize", "abc"))
	if !strings.Contains(both["errortext"].(string), "for parameter page") {
		t.Errorf("both wrong: %v", both)
	}
}
