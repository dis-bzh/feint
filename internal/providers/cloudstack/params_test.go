package cloudstack_test

import (
	"encoding/json"
	"os"
	"testing"
)

// What a client builds a request from is what listApis says of the command: its
// parameters, their types, which are required, and whether it is asynchronous. The
// fixture is that answer from a 4.23.0.0 management server, for the commands this
// pack serves. The first version of the pack disagreed with it three times (a
// required templateid, a listall on listZones, a required jobid), all found by the
// comparison this test keeps.
//
// One direction is strict and the other is not, and the difference is deliberate.
// Everything the pack declares must exist upstream with the same type and the same
// required flag: a parameter the server has never heard of is an invention. What
// the server declares and the pack does not is the surface not served yet, which
// is most of it, except for a required parameter: a request that omits one is
// refused upstream, so a pack that does not know it would accept what it must not.
func TestTheServedCommandsDeclareWhatTheServerDeclares(t *testing.T) {
	raw, err := os.ReadFile("testdata/simulator-4.23.0.0-listapis.json")
	if err != nil {
		t.Fatal(err)
	}
	var real struct {
		Commands map[string]struct {
			IsAsync bool `json:"isasync"`
			Params  map[string]struct {
				Type     string `json:"type"`
				Required bool   `json:"required"`
			} `json:"params"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(raw, &real); err != nil {
		t.Fatal(err)
	}

	c := newClient(t)
	answer := c.ok("GET", signed("listApis"))
	apis, _ := answer["api"].([]any)
	if len(apis) == 0 {
		t.Fatal("listApis described nothing, so nothing was compared")
	}
	mine := map[string]map[string]any{}
	for _, a := range apis {
		m := a.(map[string]any)
		mine[m["name"].(string)] = m
	}

	for name, want := range real.Commands {
		got, ok := mine[name]
		if !ok {
			t.Errorf("%s: recorded upstream and not served: re-record, or serve it", name)
			continue
		}
		if got["isasync"] != want.IsAsync {
			t.Errorf("%s: isasync is %v here and %v upstream", name, got["isasync"], want.IsAsync)
		}
		declared := map[string]bool{}
		for _, p := range got["params"].([]any) {
			pm := p.(map[string]any)
			pname := pm["name"].(string)
			declared[pname] = true
			up, ok := want.Params[pname]
			if !ok {
				t.Errorf("%s.%s: a parameter the server does not declare", name, pname)
				continue
			}
			if pm["type"] != up.Type {
				t.Errorf("%s.%s: type %v here, %s upstream", name, pname, pm["type"], up.Type)
			}
			if pm["required"] != up.Required {
				t.Errorf("%s.%s: required is %v here and %v upstream", name, pname, pm["required"], up.Required)
			}
		}
		for pname, up := range want.Params {
			if up.Required && !declared[pname] {
				t.Errorf("%s.%s: required upstream and not declared here", name, pname)
			}
		}
	}
	for name := range mine {
		if _, ok := real.Commands[name]; !ok {
			t.Errorf("%s: served and not recorded upstream, so it was never compared", name)
		}
	}
}
