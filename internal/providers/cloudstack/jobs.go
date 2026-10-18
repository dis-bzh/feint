package cloudstack

import (
	"encoding/json"
	"net/url"

	"github.com/stephrobert/feint/internal/core/resource"
)

// Job statuses, CloudStack's own numbers.
const (
	jobPending = 0
	jobDone    = 1
	jobFailed  = 2
)

// newJob records the job an asynchronous command returned and gives back its id.
//
// The job is settled before the client first asks. docs/limits.md carries the
// consequence: a client that waits on a pending job is waiting for information
// nothing here produces, which is the same decision the other packs make for
// lifecycle transitions.
func (p *Pack) newJob(out outcome) string {
	id := p.env.NewID()
	now := p.env.Now()
	job := resource.New(id, kindJob, tenant(), "done", now)
	job.Attrs["cmd"] = out.cmd
	job.Attrs["jobstatus"] = jobDone
	// The result is a document the job was given at completion, not a live view
	// of anything: it is stored serialised, and the difference is not cosmetic.
	// A machine's view carries its address, and a map holding it in the store
	// reads as a second holder of that address to the invariant sweep (and would
	// to any reader of a snapshot), when the job only remembers what it was told.
	raw, _ := json.Marshal(out.result)
	job.Attrs["jobresult"] = string(raw)
	job.Attrs["completed"] = created(now)
	p.env.Store.Put(job)
	return id
}

func (p *Pack) queryAsyncJobResult(a url.Values) (map[string]any, *apiError) {
	job, ok := p.env.Store.Get(Name, kindJob, a.Get("jobid"))
	if !ok {
		return nil, unknownEntity("jobid", a.Get("jobid"))
	}
	status := resource.Int(job, "jobstatus")
	body := map[string]any{
		"accountid": domainID, "userid": domainID,
		"cmd":           attr(job, "cmd"),
		"jobstatus":     status,
		"jobprocstatus": 0,
		"jobresultcode": 0,
		"created":       created(job.Created),
		"jobid":         job.ID,
	}
	if status != jobPending {
		body["jobresulttype"] = "object"
		var result map[string]any
		_ = json.Unmarshal([]byte(attr(job, "jobresult")), &result)
		body["jobresult"] = result
		body["completed"] = attr(job, "completed")
	}
	return body, nil
}
