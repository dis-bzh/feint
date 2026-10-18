package emulator

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Mounted is one route and the pack that owns it.
type Mounted struct {
	Provider string
	Route    Route
}

// Table answers one question: which upstream operation does this request
// address?
//
// It exists because two readers need that answer from opposite sides. NewServer
// asks it while mounting handlers, from inside the process that will answer; and
// `feint proxy` (internal/proxy, X-2) asks it about an exchange it only watches
// go past, between a real client and a real cloud it does not serve.
//
// A second table written by hand in the proxy would be a copy of the packs' own
// route lists, and copies diverge silently. The failure would be quiet and
// misleading rather than loud: the day a pack moved a path, the proxy would
// report an empty operation for that exchange — and an empty operation already
// means something here, "a real client walked a route no pack claims", which is
// a finding this repository acts on. A copy would manufacture that finding.
//
// The matching is net/http's own rather than a prefix comparison of this
// package's invention. Route.Path is a 1.22 pattern
// ("/instance/v1/zones/{zone}/servers"), so ServeMux already resolves a concrete
// path to the pattern serving it, wildcards and precedence included; Handler
// reports which pattern matched, and that is the key back to the route.
//
// One extension of this package's own, because net/http cannot express it: a
// trailing "{id}:action" segment. REST dialects that put the verb in the same
// path segment as the identifier (Exoscale writes PUT /instance/{id}:stop) do
// not fit a ServeMux wildcard, which always spans the whole segment. Such a
// route is indexed here under its declared pseudo-pattern, mounted under the
// base pattern ("PUT /instance/{id}"), and resolved by the suffix after the
// first colon of the final segment. The parsing is dialect-neutral — nothing
// here knows which provider writes its verbs that way.
//
// A second extension, for the dialect that does not put the verb in the path at
// all: a Route.Path of "/client/api?command=listZones" is a route selected by a
// parameter. CloudStack serves its whole API on one URL and names the operation
// in a `command` parameter, in the query of a GET or in the form body of a POST.
// The text before the "?" is the pattern mounted; the key and value after it say
// which request that route answers. As with the action suffix, the parsing is
// dialect-neutral: nothing here knows which provider writes its verbs there.
//
// TestTheTableNamesTheOperationAServeMuxWouldRoute holds the plain half;
// TestActionSuffixRoutesAreNamedAndServed holds the extension;
// TestSelectorRoutesAreNamedAndServed holds the selector.
type Table struct {
	mux *http.ServeMux
	at  map[string]Mounted
	// groups indexes what is mounted at one base pattern: the plain route under
	// action "", and one entry per ":action" suffix.
	groups map[string]*actionGroup
}

// actionGroup is everything mounted at one "METHOD /path/{wildcard}" pattern.
type actionGroup struct {
	// wildcard is the name of the trailing path wildcard, so a dispatcher can
	// re-set its value once the action suffix is stripped. Empty when the base
	// pattern does not end in a wildcard, in which case no action route can
	// exist under it either.
	wildcard string
	// selector is the parameter whose value picks the route, for a base mounted
	// by "?key=value" routes. Empty otherwise. A group is one or the other: a
	// base pattern is addressed by a path suffix or by a parameter, never both.
	selector string
	routes   map[string]Mounted
}

// actionSegment matches a final "{id}:action" path segment: a whole-segment
// wildcard, a colon, and the action name.
var actionSegment = regexp.MustCompile(`^\{([A-Za-z_][A-Za-z0-9_]*)\}:([A-Za-z0-9-]+)$`)

// plainWildcard matches a final "{id}" segment of an ordinary pattern.
var plainWildcard = regexp.MustCompile(`^\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// splitAction cuts a route path into the pattern net/http can mount and the
// action suffix this package resolves itself. Action is empty for an ordinary
// path.
func splitAction(path string) (base, wildcard, action string) {
	if b, _, _, ok := splitSelector(path); ok {
		return b, "", ""
	}
	i := strings.LastIndex(path, "/")
	segment := path[i+1:]
	if m := actionSegment.FindStringSubmatch(segment); m != nil {
		return path[:i+1] + "{" + m[1] + "}", m[1], m[2]
	}
	if m := plainWildcard.FindStringSubmatch(segment); m != nil {
		return path, m[1], ""
	}
	return path, "", ""
}

// splitSelector cuts "/client/api?command=listZones" into the pattern net/http
// can mount, the parameter that selects within it and the value that picks this
// route. ok is false for an ordinary path.
func splitSelector(path string) (base, key, value string, ok bool) {
	base, query, found := strings.Cut(path, "?")
	if !found {
		return path, "", "", false
	}
	key, value, found = strings.Cut(query, "=")
	if !found || key == "" || value == "" {
		return path, "", "", false
	}
	return base, key, value, true
}

// selectorValue reads the parameter that selects a route: from the query, and
// failing that from a form-encoded body, which is where a POST puts it.
//
// The body is read here and put back, for the reason Outscale's dry-run probe
// does the same: the handler owns it afterwards, and a route that could not
// read its own form would answer every POST with a missing parameter. Bounded
// by MaxBody like every other body.
func selectorValue(r *http.Request, key string) string {
	if v := r.URL.Query().Get(key); v != "" {
		return v
	}
	if r.Method != http.MethodPost || r.Body == nil {
		return ""
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/x-www-form-urlencoded" {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBody))
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
	if err != nil {
		return ""
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return ""
	}
	return form.Get(key)
}

// NewTable indexes every route the packs serve.
//
// It fails when two packs claim the same route, which is the check that used to
// live inline in NewServer: without it net/http panics at a random point in
// start-up, naming the pattern and neither of the packs that wanted it.
func NewTable(packs ...Pack) (*Table, error) {
	t := &Table{mux: http.NewServeMux(), at: make(map[string]Mounted), groups: make(map[string]*actionGroup)}
	for _, p := range packs {
		for _, r := range p.Routes() {
			pattern := r.Method + " " + r.Path
			if owner, dup := t.at[pattern]; dup {
				return nil, fmt.Errorf("route %q claimed by both %q and %q", pattern, owner.Provider, p.Name())
			}
			t.at[pattern] = Mounted{Provider: p.Name(), Route: r}

			base, wildcard, action := splitAction(r.Path)
			_, selKey, selValue, selected := splitSelector(r.Path)
			if selected {
				action = selValue
			}
			basePattern := r.Method + " " + base
			g := t.groups[basePattern]
			if g == nil {
				g = &actionGroup{routes: make(map[string]Mounted)}
				t.groups[basePattern] = g
				// The handler is never called: this mux is consulted with Handler,
				// which resolves and returns rather than serves. Registering the
				// pack's real handler here would give the table a second live copy of
				// every route, and the proxy — which must never answer a request —
				// would be holding one.
				t.mux.HandleFunc(basePattern, func(http.ResponseWriter, *http.Request) {})
			}
			if wildcard != "" {
				g.wildcard = wildcard
			}
			if selected {
				if _, plain := g.routes[""]; plain {
					return nil, fmt.Errorf("route %q is mounted beside a plain route at %q: a base is addressed one way", pattern, base)
				}
				if g.selector != "" && g.selector != selKey {
					return nil, fmt.Errorf("route %q selects by %q where %q is already selected by %q", pattern, selKey, base, g.selector)
				}
				g.selector = selKey
			} else if g.selector != "" {
				return nil, fmt.Errorf("route %q is mounted beside routes selected by %q: a base is addressed one way", pattern, g.selector)
			}
			if owner, dup := g.routes[action]; dup {
				return nil, fmt.Errorf("route %q claimed by both %q and %q", pattern, owner.Provider, p.Name())
			}
			g.routes[action] = Mounted{Provider: p.Name(), Route: r}
		}
	}
	return t, nil
}

// Lookup names the route mounted at a request's address.
//
// The second return is false when nothing is mounted there, which the caller
// must not confuse with a route whose Operation is empty: rule 2 forbids the
// latter and TestEveryRouteDeclaresAnOperation enforces it, so in practice
// "false" is the only way an operation can be unknown.
func (t *Table) Lookup(r *http.Request) (Mounted, bool) {
	_, pattern := t.mux.Handler(r)
	g, ok := t.groups[pattern]
	if !ok {
		return Mounted{}, false
	}
	m, ok := g.routes[requestAction(g, r)]
	return m, ok
}

// requestAction extracts the action suffix of a request path, for a group that
// serves at least one action. A group with none never parses the path at all,
// so a colon inside an ordinary identifier keeps meaning nothing.
func requestAction(g *actionGroup, r *http.Request) string {
	if g.selector != "" {
		return selectorValue(r, g.selector)
	}
	path := r.URL.Path
	if len(g.routes) == 1 {
		if _, plainOnly := g.routes[""]; plainOnly {
			return ""
		}
	}
	segment := path[strings.LastIndex(path, "/")+1:]
	_, action, _ := strings.Cut(segment, ":")
	return action
}

// All returns every mounted route, keyed by the "METHOD /path" pattern the pack
// declared — the pseudo-pattern, for a route with an action suffix.
func (t *Table) All() map[string]Mounted {
	out := make(map[string]Mounted, len(t.at))
	for pattern, m := range t.at {
		out[pattern] = m
	}
	return out
}
