package cloudstack_test

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
)

// The recording is what a CloudStack 4.23.0.0 management server (the official
// simulator image) answered to tools/conformance/cloudstack/oracle.py on
// 2026-10-18: 85 steps, every raw answer kept. It is the one place the pack's
// tests learn what the real server does, and three things read it:
//
//   - shapes_test.go holds the field set of every kind of answer to it;
//   - contract_test.go replays it through the contract, to show the artefact
//     accepts what the server really said;
//   - tools/conformance/cloudstack/record_wrappers.py reads the same file to say how
//     the wire wraps each command's answer.
//
// Re-record it by running the oracle against a simulator; compare.py then says
// whether the pack still agrees with the fresh answers.
type recorded struct {
	Label   string         `json:"label"`
	Command string         `json:"command"`
	Status  int            `json:"status"`
	Body    map[string]any `json:"body"`
}

func loadRecording(t *testing.T) []recorded {
	t.Helper()
	raw, err := os.ReadFile("testdata/simulator-4.23.0.0-recording.json")
	if err != nil {
		t.Fatal(err)
	}
	var steps []recorded
	if err := json.Unmarshal(raw, &steps); err != nil {
		t.Fatal(err)
	}
	if len(steps) == 0 {
		t.Fatal("an empty recording compares nothing")
	}
	return steps
}

func recordingByLabel(t *testing.T) map[string]recorded {
	t.Helper()
	out := map[string]recorded{}
	for _, s := range loadRecording(t) {
		out[s.Label] = s
	}
	return out
}

// shape is the set of field paths of one kind of answer: every path anywhere in
// it, and the paths that every element of a list carries.
//
// The second set exists because a list is a population, not an object. The server
// answers `since` on 793 of 5316 parameters in listApis, so a pack that never
// writes one is not omitting a field, and a pack that writes it where the server
// never does is inventing one. Held apart, the two cases are told apart: a path
// must be answered when the server always answers it, and may be answered only
// where the server does at least once.
type shape struct {
	all    map[string]bool
	always map[string]bool
}

func shapeOf(v any) shape {
	all, always := walk(v, "")
	return shape{all, always}
}

// walk returns the paths of a value. The free-form "details" map is one opaque
// path: its keys are data, not shape.
func walk(v any, prefix string) (all, always map[string]bool) {
	all, always = map[string]bool{}, map[string]bool{}
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			all[p], always[p] = true, true
			if k == "details" {
				continue
			}
			a, al := walk(child, p)
			for q := range a {
				all[q] = true
			}
			for q := range al {
				always[q] = true
			}
		}
	case []any:
		first := true
		for _, child := range x {
			a, al := walk(child, prefix+"[]")
			for q := range a {
				all[q] = true
			}
			if first {
				for q := range al {
					always[q] = true
				}
				first = false
				continue
			}
			for q := range always {
				if strings.HasPrefix(q, prefix+"[]") && !al[q] {
					delete(always, q)
				}
			}
		}
	}
	return all, always
}

// difference lists what a pack's answer lacks and what it adds, against the
// recorded one: omitted are paths the server always answers and the pack never
// does, invented are paths the pack answers and the server never does.
func difference(real, mine shape) (omitted, invented []string) {
	for p := range real.always {
		if !mine.all[p] {
			omitted = append(omitted, p)
		}
	}
	for p := range mine.all {
		if !real.all[p] {
			invented = append(invented, p)
		}
	}
	sort.Strings(omitted)
	sort.Strings(invented)
	return omitted, invented
}

type conformance struct {
	Exercised  int                 `json:"exercised"`
	Contracts  []string            `json:"contracts"`
	Violations map[string][]string `json:"violations"`
}

func conformanceReport(t *testing.T, base string) conformance {
	t.Helper()
	resp, err := http.Get(base + "/_feint/conformance")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out conformance
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
