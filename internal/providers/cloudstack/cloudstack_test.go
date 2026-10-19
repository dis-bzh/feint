package cloudstack_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stephrobert/feint/internal/core/emulator"
	"github.com/stephrobert/feint/internal/providers/cloudstack"
)

const (
	zone     = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c01"
	template = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c03"
	small    = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c04"
)

// client drives the emulator over HTTP the way the real ones do: GET with the
// command in the query (go-cloudstack, cs), POST with it in a form body (cmk).
type client struct {
	t   *testing.T
	srv *httptest.Server
}

func newClient(t *testing.T) *client {
	t.Helper()
	env := emulator.DefaultEnv()
	s, err := emulator.NewServer(env, cloudstack.New(env))
	if err != nil {
		t.Fatalf("mounting the pack: %v", err)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &client{t: t, srv: srv}
}

func signed(command string, kv ...string) url.Values {
	v := url.Values{"command": {command}, "response": {"json"}, "apikey": {"k"}, "signature": {"s"}}
	for i := 0; i < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

// call returns the HTTP status and the body under the command's response key.
func (c *client) call(method string, v url.Values) (int, map[string]any) {
	c.t.Helper()
	var resp *http.Response
	var err error
	if method == http.MethodGet {
		resp, err = http.Get(c.srv.URL + "/client/api?" + v.Encode())
	} else {
		resp, err = http.Post(c.srv.URL+"/client/api", "application/x-www-form-urlencoded", strings.NewReader(v.Encode()))
	}
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, v.Get("command"), err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var envelope map[string]map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		c.t.Fatalf("%s %s answered %d with a body that is not an envelope: %s", method, v.Get("command"), resp.StatusCode, raw)
	}
	if len(envelope) != 1 {
		c.t.Fatalf("an answer is one key, got %d: %s", len(envelope), raw)
	}
	for key, body := range envelope {
		if want := strings.ToLower(v.Get("command")) + "response"; key != want && key != "errorresponse" {
			c.t.Fatalf("answer keyed %q, want %q", key, want)
		}
		return resp.StatusCode, body
	}
	return 0, nil
}

func (c *client) ok(method string, v url.Values) map[string]any {
	c.t.Helper()
	status, body := c.call(method, v)
	if status != http.StatusOK {
		c.t.Fatalf("%s %s answered %d: %v", method, v.Get("command"), status, body)
	}
	return body
}

// settle polls a job to its end, as every client does.
func (c *client) settle(method string, body map[string]any) map[string]any {
	c.t.Helper()
	jobID, _ := body["jobid"].(string)
	if jobID == "" {
		c.t.Fatalf("an asynchronous command answered without a jobid: %v", body)
	}
	job := c.ok(method, signed("queryAsyncJobResult", "jobid", jobID))
	if status, _ := job["jobstatus"].(float64); status != 1 {
		c.t.Fatalf("job did not settle: %v", job)
	}
	result, _ := job["jobresult"].(map[string]any)
	return result
}

func TestAMachineLivesItsWholeLifeOverGetAndPost(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			c := newClient(t)

			zones := c.ok(method, signed("listZones"))
			if zones["count"].(float64) != 1 {
				t.Fatalf("zones: %v", zones)
			}

			deploy := c.ok(method, signed("deployVirtualMachine",
				"zoneid", zone, "templateid", template, "serviceofferingid", small, "name", "web-1"))
			id := deploy["id"].(string)
			vm := c.settle(method, deploy)["virtualmachine"].(map[string]any)
			if vm["id"] != id || vm["state"] != "Running" || vm["name"] != "web-1" {
				t.Fatalf("deployed machine: %v", vm)
			}
			nic := vm["nic"].([]any)[0].(map[string]any)
			if nic["ipaddress"] != "10.1.1.10" || nic["isdefault"] != true {
				t.Fatalf("nic: %v", nic)
			}

			if got := c.ok(method, signed("listVirtualMachines", "name", "web-1"))["count"]; got != float64(1) {
				t.Fatalf("list by name: %v", got)
			}

			stop := c.settle(method, c.ok(method, signed("stopVirtualMachine", "id", id)))
			if stop["virtualmachine"].(map[string]any)["state"] != "Stopped" {
				t.Fatalf("stop: %v", stop)
			}
			start := c.settle(method, c.ok(method, signed("startVirtualMachine", "id", id)))
			if start["virtualmachine"].(map[string]any)["state"] != "Running" {
				t.Fatalf("start: %v", start)
			}

			// Destroyed, still listed; then expunged, gone.
			gone := c.settle(method, c.ok(method, signed("destroyVirtualMachine", "id", id)))
			if gone["virtualmachine"].(map[string]any)["state"] != "Destroyed" {
				t.Fatalf("destroy: %v", gone)
			}
			if got := c.ok(method, signed("listVirtualMachines", "id", id))["count"]; got != float64(1) {
				t.Fatalf("a destroyed machine must stay listed until expunged: %v", got)
			}
			c.settle(method, c.ok(method, signed("destroyVirtualMachine", "id", id, "expunge", "true")))
			if empty := c.ok(method, signed("listVirtualMachines", "id", id)); len(empty) != 0 {
				t.Fatalf("an empty list is an empty object upstream, got %v", empty)
			}
		})
	}
}

func TestEachDeployTakesTheNextFreeAddress(t *testing.T) {
	c := newClient(t)
	ips := map[string]bool{}
	for i := 0; i < 3; i++ {
		d := c.ok("GET", signed("deployVirtualMachine", "zoneid", zone, "templateid", template, "serviceofferingid", small))
		vm := c.settle("GET", d)["virtualmachine"].(map[string]any)
		ips[vm["nic"].([]any)[0].(map[string]any)["ipaddress"].(string)] = true
	}
	if len(ips) != 3 {
		t.Fatalf("three machines shared addresses: %v", ips)
	}
}

func TestTheErrorsSpeakCloudStack(t *testing.T) {
	c := newClient(t)
	cases := []struct {
		name   string
		v      url.Values
		status int
		text   string
	}{
		{"no credentials", url.Values{"command": {"listZones"}, "response": {"json"}}, 401, "unable to verify user credentials"},
		{"missing parameter", signed("deployVirtualMachine"), 431, "missing parameter serviceofferingid"},
		{"unknown entity", signed("deployVirtualMachine", "zoneid", "nope", "templateid", template, "serviceofferingid", small), 431, "Invalid parameter zoneid value=nope"},
		{"unknown job", signed("queryAsyncJobResult", "jobid", "nope"), 431, "Invalid parameter jobid"},
		{"no job id", signed("queryAsyncJobResult"), 431, "Expected parameter job id"},
		{"unknown command", signed("createUniverse"), 432, "createUniverse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := c.call("GET", tc.v)
			if status != tc.status || body["errorcode"] != float64(tc.status) {
				t.Fatalf("status %d: %v", status, body)
			}
			if text, _ := body["errortext"].(string); !strings.Contains(text, tc.text) {
				t.Fatalf("errortext %q lacks %q", text, tc.text)
			}
		})
	}
}

func TestAnUnservedCommandIsMarkedAsNotEmulated(t *testing.T) {
	c := newClient(t)
	resp, err := http.Get(c.srv.URL + "/client/api?" + signed("createUniverse").Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Header.Get(emulator.NotEmulatedHeader) != cloudstack.Name {
		t.Fatalf("refusal not marked: %v", resp.Header)
	}
}

// listApis is what cmk builds its whole command set from, so it has to describe
// exactly what is served: every route, no more.
func TestListApisDescribesExactlyWhatIsServed(t *testing.T) {
	c := newClient(t)
	apis := c.ok("POST", signed("listApis"))["api"].([]any)

	described := map[string]map[string]any{}
	for _, a := range apis {
		m := a.(map[string]any)
		described[m["name"].(string)] = m
	}
	env := emulator.DefaultEnv()
	served := map[string]bool{}
	for _, r := range cloudstack.New(env).Routes() {
		_, name, _ := strings.Cut(r.Path, "command=")
		served[name] = true
	}
	for name := range served {
		if described[name] == nil {
			t.Errorf("%s is served and not described", name)
		}
	}
	for name := range described {
		if !served[name] {
			t.Errorf("%s is described and not served", name)
		}
	}
	if described["deployVirtualMachine"]["isasync"] != true || described["listZones"]["isasync"] != false {
		t.Errorf("isasync is wrong: deploy=%v list=%v", described["deployVirtualMachine"]["isasync"], described["listZones"]["isasync"])
	}

	one := c.ok("GET", signed("listApis", "name", "listZones"))
	if one["count"] != float64(1) {
		t.Fatalf("listApis name filter: %v", one)
	}
}

func TestTheEnvironmentPointsBothFamiliesOfClient(t *testing.T) {
	env := cloudstack.New(emulator.DefaultEnv()).Env("http://127.0.0.1:4599")
	for _, k := range []string{"CLOUDSTACK_API_URL", "CLOUDSTACK_ENDPOINT"} {
		if env.Vars[k] != "http://127.0.0.1:4599/client/api" {
			t.Errorf("%s = %q", k, env.Vars[k])
		}
	}
	for _, k := range []string{"CLOUDSTACK_API_KEY", "CLOUDSTACK_SECRET_KEY", "CLOUDSTACK_KEY", "CLOUDSTACK_SECRET"} {
		if env.Vars[k] == "" {
			t.Errorf("%s is empty", k)
		}
	}
}

// cmk spells the key apiKey and go-cloudstack apikey; the management server
// reads both, so a pack that answered 401 to one would be unreachable by it.
func TestTheApiKeyIsReadWhateverItsCase(t *testing.T) {
	c := newClient(t)
	v := url.Values{"command": {"listZones"}, "response": {"json"}, "apiKey": {"k"}, "signature": {"s"}}
	if body := c.ok("GET", v); body["count"] != float64(1) {
		t.Fatalf("zones: %v", body)
	}
}

// callWhole is call() without unwrapping the envelope.
func (c *client) callWhole(method string, v map[string][]string) (int, map[string]any) {
	c.t.Helper()
	form := url.Values(v)
	var resp *http.Response
	var err error
	if method == http.MethodGet {
		resp, err = http.Get(c.srv.URL + "/client/api?" + form.Encode())
	} else {
		resp, err = http.Post(c.srv.URL+"/client/api", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	}
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		c.t.Fatalf("not JSON (%d): %s", resp.StatusCode, raw)
	}
	return resp.StatusCode, body
}
