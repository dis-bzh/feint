package cloudstack_test

import (
	"path/filepath"
	"testing"

	"github.com/stephrobert/feint/internal/contract"
)

func loadContract(t *testing.T) *contract.Doc {
	t.Helper()
	doc, err := contract.Load(filepath.Join("..", "..", "..", "contracts", "cloudstack.json"))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// jsonTypeOf says what a listApis type is on the wire, the way the contract
// records it (tools/contract/extract-cloudstack.py): the vocabulary the pack answers
// listApis in is the server's, the contract's is JSON's.
func jsonTypeOf(upstream string) string {
	switch upstream {
	case "uuid", "string", "date":
		return "string"
	case "boolean":
		return "boolean"
	case "integer", "long", "short":
		return "integer"
	case "list":
		return "array"
	case "map", "object":
		return "object"
	}
	return ""
}

// What a client builds a request from is what listApis says of the command: its
// parameters, their types, which are required, the verb it prefers and whether it
// is asynchronous. The contract holds that, extracted from the server's own answer,
// and this holds the pack's table to it. The first version of the pack disagreed
// with the server three times (a required templateid, a listall on listZones, a
// required jobid), all found by the comparison this keeps.
//
// One direction is strict and the other is not, and the difference is deliberate.
// Everything the pack declares must exist upstream with the same type and the same
// required flag: a parameter the server has never heard of is an invention. What
// the server declares and the pack does not is the surface not served yet, which
// is most of it, except for a required parameter: a request that omits one is
// refused upstream, so a pack that does not know it would accept what it must not.
func TestTheServedCommandsDeclareWhatTheServerDeclares(t *testing.T) {
	doc := loadContract(t)
	c := newClient(t)
	apis, _ := c.ok("GET", signed("listApis"))["api"].([]any)
	if len(apis) == 0 {
		t.Fatal("listApis described nothing, so nothing was compared")
	}

	for _, a := range apis {
		got := a.(map[string]any)
		name := got["name"].(string)
		op, ok := doc.Operations[name]
		if !ok {
			t.Errorf("%s: served and absent from the contract", name)
			continue
		}
		if got["isasync"] != op.Async {
			t.Errorf("%s: isasync is %v here and %v upstream", name, got["isasync"], op.Async)
		}
		if got["httprequesttype"] != op.Method {
			t.Errorf("%s: the preferred verb is %v here and %s upstream", name, got["httprequesttype"], op.Method)
		}
		required := map[string]bool{}
		for _, r := range op.QueryRequired {
			required[r] = true
		}
		declared := map[string]bool{}
		for _, p := range got["params"].([]any) {
			pm := p.(map[string]any)
			pname := pm["name"].(string)
			declared[pname] = true
			up, ok := op.Query[pname]
			if !ok {
				t.Errorf("%s.%s: a parameter the server does not declare", name, pname)
				continue
			}
			if want := jsonTypeOf(pm["type"].(string)); want != up.Type {
				t.Errorf("%s.%s: type %v here is a %q, and upstream says %q", name, pname, pm["type"], want, up.Type)
			}
			if pm["required"] != required[pname] {
				t.Errorf("%s.%s: required is %v here and %v upstream", name, pname, pm["required"], required[pname])
			}
		}
		for pname := range required {
			if !declared[pname] {
				t.Errorf("%s.%s: required upstream and not declared here", name, pname)
			}
		}
	}
}
