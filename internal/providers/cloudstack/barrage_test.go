package cloudstack_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stephrobert/feint/internal/core/emulator"
	"github.com/stephrobert/feint/internal/core/store"
	"github.com/stephrobert/feint/internal/core/store/storetest"
	"github.com/stephrobert/feint/internal/providers/cloudstack"
)

// The barrage, and the invariant sweep that follows it.
//
// The sweep lives in the core, keyed on nothing but resource.Resource, so no
// pack defines its own idea of coherent. What is CloudStack's own is the traffic:
// a deploy per worker racing for the guest network's addresses, then the
// lifecycle verbs on machines the other workers are creating.

const barrageWorkers = 8

func newBarrageServer(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	env := emulator.DefaultEnv()
	srv, err := emulator.NewServer(env, cloudstack.New(env))
	if err != nil {
		t.Fatalf("build the server: %v", err)
	}
	return srv.Handler(), env.Store
}

// callRaw is call() without the testing.T: a goroutine may not call t.Fatalf.
func callRaw(h http.Handler, v url.Values) (int, string) {
	req := httptest.NewRequest(http.MethodGet, "/client/api?"+v.Encode(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestACloudStackBarrageLeavesTheStoreCoherent(t *testing.T) {
	h, st := newBarrageServer(t)

	var wg sync.WaitGroup
	problems := make(chan string, barrageWorkers*16)
	addresses := make(chan string, barrageWorkers*2)

	for w := range barrageWorkers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := range 2 {
				status, body := callRaw(h, signed("deployVirtualMachine",
					"zoneid", zone, "templateid", template, "serviceofferingid", small,
					"name", fmt.Sprintf("barrage-%d-%d", worker, i)))
				if status != http.StatusOK {
					problems <- fmt.Sprintf("w%d-%d: deploy answered %d: %s", worker, i, status, body)
					continue
				}
				id := between(body, `"id":"`, `"`)
				if id == "" {
					problems <- fmt.Sprintf("w%d-%d: the deploy names no machine: %s", worker, i, body)
					continue
				}
				// Walked through every verb, and the last left Destroyed rather
				// than expunged on every other round, so the sweep has both a
				// settled machine and a held address to look at.
				for _, cmd := range []string{"stopVirtualMachine", "startVirtualMachine", "stopVirtualMachine"} {
					if status, body := callRaw(h, signed(cmd, "id", id)); status != http.StatusOK {
						problems <- fmt.Sprintf("w%d-%d: %s answered %d: %s", worker, i, cmd, status, body)
					}
				}
				if i == 0 {
					callRaw(h, signed("destroyVirtualMachine", "id", id, "expunge", "true"))
				} else {
					callRaw(h, signed("destroyVirtualMachine", "id", id))
				}
			}
		}(w)
	}
	wg.Wait()
	close(problems)
	close(addresses)

	var refused []string
	for p := range problems {
		refused = append(refused, p)
	}
	if len(refused) > 0 {
		t.Errorf("%d request(s) the barrage did not expect to fail:\n%s", len(refused), strings.Join(refused, "\n"))
	}

	// What the store holds must be what a snapshot can give back (#567).
	if found := storetest.GoShapes(st.All()); len(found) != 0 {
		t.Errorf("the barrage stored %d value(s) a snapshot cannot give back as themselves:\n%s",
			len(found), strings.Join(found, "\n"))
	}
	// Two machines holding one address is the defect the addressing lock exists
	// for, and it is what the sweep's address invariant reads.
	if found := storetest.Sweep(st.All(), nil, nil); len(found) != 0 {
		t.Errorf("the store is incoherent after the barrage:\n%s", strings.Join(found, "\n"))
	}
	// The sweep passes over a store where nothing holds an address as easily as
	// over one where nothing collides, so name what it was given.
	held := 0
	for _, r := range st.All() {
		if r.Kind == "virtualmachine" {
			held++
		}
	}
	if held != barrageWorkers {
		t.Errorf("the barrage left %d machines, want %d (one destroyed per worker): the sweep ran over the wrong store", held, barrageWorkers)
	}
}

func between(s, open, close string) string {
	_, rest, ok := strings.Cut(s, open)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, close)
	return v
}

// An armed fault is rendered in CloudStack's own envelope, under the response
// key of the command it hit, so cs and go-cloudstack decode it as the error it
// is rather than as an empty answer.
func TestAnInjectedFaultIsACloudStackError(t *testing.T) {
	c := newClient(t)
	req, _ := http.NewRequest(http.MethodPut, c.srv.URL+"/_feint/faults",
		strings.NewReader(`{"faults":[{"operation":"cs/API.listZones","status":503}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("arming the fault answered %d", resp.StatusCode)
	}

	status, body := c.call("GET", signed("listZones"))
	if status != 503 || body["errorcode"] != float64(503) {
		t.Fatalf("status %d: %v", status, body)
	}
	if text, _ := body["errortext"].(string); !strings.Contains(text, "fault rule") {
		t.Fatalf("an injected failure must say it is one: %q", text)
	}
}
