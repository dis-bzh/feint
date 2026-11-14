package emulator_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/stephrobert/feint/internal/core/emulator"
	"github.com/stephrobert/feint/internal/providers/exoscale"
	"github.com/stephrobert/feint/internal/providers/outscale"
	"github.com/stephrobert/feint/internal/providers/scaleway"
)

// wildcard matches a net/http 1.22 path parameter, "{zone}" or "{id}".
var wildcard = regexp.MustCompile(`\{[^}/]+\}`)

// concrete turns a route pattern into a path a client could actually request.
//
// The value put in each wildcard is deliberately not a valid identifier for
// anything: the table resolves an address, and it must not need the resource to
// exist. If a substitution ever mattered, that would itself be the defect.
func concrete(pattern string) string {
	return wildcard.ReplaceAllString(pattern, "x")
}

// The table names the operation for every address a pack serves.
//
// It is driven from the packs' own route lists rather than from a fixture, so a
// pack that adds a route adds a case here, and a pack that moves a path moves
// its case with it. That is the whole point of the type: `feint proxy` names an
// exchange it only watches, and the answer has to be the one this emulator would
// have given.
//
// The failing half is the one worth stating: with Lookup comparing prefixes
// instead of asking the mux, /instance/v1/zones/x/servers/x resolves to
// ListServers rather than GetServer, because a prefix match cannot tell a
// collection from a member.
func TestTheTableNamesTheOperationAServeMuxWouldRoute(t *testing.T) {
	env := emulator.DefaultEnv()
	packs := []emulator.Pack{scaleway.New(env), outscale.New(env), exoscale.New(env)}

	table, err := emulator.NewTable(packs...)
	if err != nil {
		t.Fatalf("build the table: %v", err)
	}

	var routes int
	for _, p := range packs {
		for _, r := range p.Routes() {
			routes++
			path := concrete(r.Path)
			req := httptest.NewRequest(r.Method, path, nil)
			m, ok := table.Lookup(req)
			if !ok {
				t.Errorf("%s %s: the table names no operation, so a transcript would report a route no pack claims",
					r.Method, path)
				continue
			}
			if m.Route.Operation != r.Operation {
				t.Errorf("%s %s: the table names %q, the pack serves %q",
					r.Method, path, m.Route.Operation, r.Operation)
			}
			if m.Provider != p.Name() {
				t.Errorf("%s %s: the table credits %q, the route belongs to %q",
					r.Method, path, m.Provider, p.Name())
			}
		}
	}
	if routes == 0 {
		t.Fatal("no route to check, so this test measures nothing")
	}

	// The refusing half. An address no pack serves must come back unknown: that
	// is what makes an empty operation in a transcript mean "a real client walked
	// a route nobody claims" rather than "the table gave up".
	for _, unclaimed := range []struct{ method, path string }{
		{http.MethodGet, "/no/such/api"},
		{http.MethodGet, "/instance/v1/zones/fr-par-1/nonexistent"},
		// The method is part of the address. A pack serving GET on a path does
		// not serve TRACE on it, and a table that ignored the verb would name an
		// operation for a call the emulator answers 405 to.
		{http.MethodTrace, "/instance/v1/zones/fr-par-1/servers"},
	} {
		if m, ok := table.Lookup(httptest.NewRequest(unclaimed.method, unclaimed.path, nil)); ok {
			t.Errorf("%s %s: the table names %q for an address no pack serves",
				unclaimed.method, unclaimed.path, m.Route.Operation)
		}
	}
}

// A trailing "{id}:action" segment routes by its suffix, both in the table and
// on the mounted server.
//
// net/http cannot express the pattern — a wildcard spans the whole segment — so
// without the table's own resolution every ":action" call lands on the plain
// "{id}" route: a stop would be named (and served) as an update, and the
// operation the coverage joins on would be wrong for every lifecycle call of a
// dialect that writes verbs this way.
func TestActionSuffixRoutesAreNamedAndServed(t *testing.T) {
	served := map[string]string{}
	handler := func(op string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			served[op] = r.PathValue("id")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}
	pack := stubPack{name: "stub", routes: []emulator.Route{
		{Method: http.MethodPut, Path: "/v9/thing/{id}", Operation: "stub/v9.update-thing", Handler: handler("update")},
		{Method: http.MethodPut, Path: "/v9/thing/{id}:stop", Operation: "stub/v9.stop-thing", Handler: handler("stop")},
		{Method: http.MethodGet, Path: "/v9/thing/{id}", Operation: "stub/v9.get-thing", Handler: handler("get")},
	}}

	table, err := emulator.NewTable(pack)
	if err != nil {
		t.Fatalf("build the table: %v", err)
	}
	for path, want := range map[string]string{
		"/v9/thing/abc":      "stub/v9.update-thing",
		"/v9/thing/abc:stop": "stub/v9.stop-thing",
	} {
		m, ok := table.Lookup(httptest.NewRequest(http.MethodPut, path, nil))
		if !ok {
			t.Fatalf("PUT %s: the table names no operation", path)
		}
		if m.Route.Operation != want {
			t.Errorf("PUT %s: named %q, want %q", path, m.Route.Operation, want)
		}
	}
	// An action nobody serves is unknown, not the plain route: reporting it as
	// update-thing would hide exactly the unserved-operation finding the
	// transcript exists to surface.
	if m, ok := table.Lookup(httptest.NewRequest(http.MethodPut, "/v9/thing/abc:reboot", nil)); ok {
		t.Errorf("PUT /v9/thing/abc:reboot: named %q for an action nobody serves", m.Route.Operation)
	}

	// The served half: the dispatcher strips the suffix, so the handler's
	// PathValue is the identifier alone.
	srv, err := emulator.NewServer(emulator.DefaultEnv(), pack)
	if err != nil {
		t.Fatalf("build the server: %v", err)
	}
	for path, op := range map[string]string{
		"/v9/thing/abc":      "update",
		"/v9/thing/abc:stop": "stop",
	} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s answered %d", path, rec.Code)
		}
		if served[op] != "abc" {
			t.Errorf("PUT %s: the %s handler saw id %q, want \"abc\"", path, op, served[op])
		}
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v9/thing/abc:reboot", nil))
	if rec.Code == http.StatusOK {
		t.Error("PUT /v9/thing/abc:reboot: an action nobody serves was served anyway")
	}
}

// The table refuses a route two packs claim, which is what keeps net/http from
// panicking mid-mount with a message naming neither pack.
func TestTheTableRefusesARouteTwoPacksClaim(t *testing.T) {
	route := emulator.Route{Method: http.MethodGet, Path: "/collision", Operation: "stub/v1.Collide", Handler: noop}
	_, err := emulator.NewTable(
		stubPack{name: "one", routes: []emulator.Route{route}},
		stubPack{name: "two", routes: []emulator.Route{route}},
	)
	if err == nil {
		t.Fatal("two packs claimed the same route and the table accepted both")
	}
	// Both names, because the message exists to say who to talk to. The version
	// this replaced reported the pattern and one pack.
	for _, want := range []string{"one", "two", "/collision"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q, so it cannot be acted on: %v", want, err)
		}
	}
}

// What naming one exchange costs.
//
// `feint proxy` calls this once per request it forwards, so the number belongs
// beside the recorder's own: the alternative design — a hand-written prefix table
// in the proxy — would be faster and would be a copy of the packs' routes, which
// is the trade this type refuses to make. Knowing the price makes the refusal a
// decision rather than an assumption.
func BenchmarkTableLookup(b *testing.B) {
	env := emulator.DefaultEnv()
	table, err := emulator.NewTable(scaleway.New(env), outscale.New(env), exoscale.New(env))
	if err != nil {
		b.Fatalf("build the table: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/instance/v1/zones/fr-par-1/servers/11111111-1111-1111-1111-111111111111", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, ok := table.Lookup(req); !ok {
			b.Fatal("the table named nothing for a route a pack serves")
		}
	}
}

// TestSelectorRoutesAreNamedAndServed holds the second extension: a dialect with
// one URL and the operation in a parameter. The table must name the operation
// from the query of a GET and from the form body of a POST, and the POST's body
// must still be whole for the handler after the table has read it.
func TestSelectorRoutesAreNamedAndServed(t *testing.T) {
	var gotBody string
	handler := func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotBody = r.Form.Get("name")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}
	pack := stubPack{name: "stub", routes: []emulator.Route{
		{Method: http.MethodGet, Path: "/api?command=listThings", Operation: "stub/API.listThings", Handler: handler},
		{Method: http.MethodPost, Path: "/api?command=listThings", Operation: "stub/API.listThings", Handler: handler},
		{Method: http.MethodPost, Path: "/api?command=makeThing", Operation: "stub/API.makeThing", Handler: handler},
	}}
	table, err := emulator.NewTable(pack)
	if err != nil {
		t.Fatalf("build the table: %v", err)
	}

	get := httptest.NewRequest(http.MethodGet, "/api?command=listThings", nil)
	if m, ok := table.Lookup(get); !ok || m.Route.Operation != "stub/API.listThings" {
		t.Errorf("GET ?command=listThings: named %q (found %v)", m.Route.Operation, ok)
	}
	form := func(command string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader("command="+command+"&name=widget"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}
	if m, ok := table.Lookup(form("makeThing")); !ok || m.Route.Operation != "stub/API.makeThing" {
		t.Errorf("POST command=makeThing in the body: named %q (found %v)", m.Route.Operation, ok)
	}
	// A command nobody serves, and a request naming none, are unknown rather than
	// the nearest route.
	for name, r := range map[string]*http.Request{
		"an unserved command": form("dropThing"),
		"no command at all":   httptest.NewRequest(http.MethodGet, "/api", nil),
	} {
		if m, ok := table.Lookup(r); ok {
			t.Errorf("%s: named %q", name, m.Route.Operation)
		}
	}

	srv, err := emulator.NewServer(emulator.DefaultEnv(), pack)
	if err != nil {
		t.Fatalf("build the server: %v", err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, form("makeThing"))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST makeThing answered %d", rec.Code)
	}
	if gotBody != "widget" {
		t.Errorf("the handler read %q from a body the table had already read, want \"widget\"", gotBody)
	}
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, form("dropThing"))
	if rec.Code == http.StatusOK {
		t.Error("an unserved command was served anyway")
	}
}

// A base is addressed one way. Mixing a selected route with a plain one on the
// same pattern would leave a request with no command answered by whichever the
// mux liked, so the table refuses the pair when it is built.
func TestASelectorRouteCannotShareABaseWithAPlainOne(t *testing.T) {
	noop := func(http.ResponseWriter, *http.Request) {}
	_, err := emulator.NewTable(stubPack{name: "stub", routes: []emulator.Route{
		{Method: http.MethodGet, Path: "/api?command=a", Operation: "stub.a", Handler: noop},
		{Method: http.MethodGet, Path: "/api", Operation: "stub.plain", Handler: noop},
	}})
	if err == nil {
		t.Fatal("a plain route beside a selected one was accepted")
	}
	// And in the other order: the pair is refused whichever is declared first.
	_, err = emulator.NewTable(stubPack{name: "stub", routes: []emulator.Route{
		{Method: http.MethodGet, Path: "/api", Operation: "stub.plain", Handler: noop},
		{Method: http.MethodGet, Path: "/api?command=a", Operation: "stub.a", Handler: noop},
	}})
	if err == nil {
		t.Fatal("a selected route beside a plain one was accepted")
	}
}
