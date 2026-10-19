package cloudstack_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stephrobert/feint/internal/contract"
	"github.com/stephrobert/feint/internal/core/emulator"
	"github.com/stephrobert/feint/internal/providers/cloudstack"
)

// Three ways the contract is held, none of which is the contract agreeing with
// itself.

// The contract accepts what the real server said. The artefact is extracted from
// listApis, which describes the fields of an answer and says nothing of how the
// wire wraps them, so the extractor closes its schemas on an assumption and types
// them on a mapping. Replaying 85 recorded answers through it is what makes that
// assumption a measurement: one field the server answered and its own listApis did
// not declare, or one type the mapping got wrong, fails here.
func TestTheRecordingValidatesAgainstTheContract(t *testing.T) {
	doc := loadContract(t)
	checked := 0
	for _, s := range loadRecording(t) {
		if s.Status != 200 || len(s.Body) != 1 {
			continue
		}
		op, known := doc.Operations[s.Command]
		if !known || op.Response == "" {
			continue
		}
		key := strings.ToLower(s.Command) + "response"
		if _, wrapped := s.Body[key]; !wrapped {
			continue // an answer under another key is an error, checked below
		}
		checked++
		if vs := doc.ValidateResponse(s.Command, s.Body); len(vs) > 0 {
			t.Errorf("step %s (%s): the server's own answer violates the contract: %v", s.Label, s.Command, vs)
		}
	}
	if checked < 40 {
		t.Fatalf("only %d recorded answers were replayed, so the replay compares too little", checked)
	}

	// And the errors the server gave, under the one envelope the contract can name.
	errors := 0
	for _, s := range loadRecording(t) {
		if _, ok := s.Body["errorresponse"]; !ok {
			continue
		}
		errors++
		if vs := doc.Validate(doc.ErrorSchema, s.Body); len(vs) > 0 {
			t.Errorf("step %s: the server's error violates %s: %v", s.Label, doc.ErrorSchema, vs)
		}
	}
	if errors == 0 {
		t.Fatal("no recorded error was replayed")
	}
}

// Every command the pack serves has the schema of its answer in the contract, which
// is the rule the extractor states: a command is given one when, and only when, an
// answer of it has been recorded. A command served without one would be answered
// unchecked, and would look checked.
func TestEveryServedCommandHasARecordedAnswer(t *testing.T) {
	doc := loadContract(t)
	for _, r := range cloudstack.New(emulator.DefaultEnv()).Routes() {
		_, name, found := doc.OperationFor(r.Operation)
		if !found {
			t.Errorf("%s: not in the contract", r.Operation)
			continue
		}
		if doc.Operations[name].Response == "" {
			t.Errorf("%s: served with no recorded answer in the contract: record it (oracle.py, record_wrappers.py) and regenerate", name)
		}
	}
}

// The pack's own answers pass the contract as they are produced. The observer
// validates every response of a provider that has a contract, and /_feint/conformance
// reports what failed, so playing the scenario against a server that has the
// contract loaded is the check, run the way the conformance suite runs it.
func TestThePackViolatesNoContractWhilePlayingEveryHistory(t *testing.T) {
	doc := loadContract(t)
	env := emulator.DefaultEnv()
	env.Contracts = map[string]*contract.Doc{cloudstack.Name: doc}
	srv, err := emulator.NewServer(env, cloudstack.New(env))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	play(t, &client{t: t, srv: ts})

	report := conformanceReport(t, ts.URL)
	if len(report.Violations) > 0 {
		t.Errorf("the pack answered what its contract refuses: %v", report.Violations)
	}
	if len(report.Contracts) == 0 || report.Exercised < 10 {
		t.Errorf("the observer checked too little for the verdict to mean anything: contracts %v, %d operations exercised", report.Contracts, report.Exercised)
	}
}
