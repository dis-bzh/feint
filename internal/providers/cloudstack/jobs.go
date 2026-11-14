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
// The job is settled before the client first asks. docs/cloudstack.md carries the
// consequence: a client that waits on a pending job is waiting for information
// nothing here produces, which is the same decision the other packs make for
// lifecycle transitions.
func (p *Pack) newJob(out outcome) string {
	id := p.env.NewID()
	now := p.env.Now()
	job := resource.New(id, kindJob, tenant(), "done", now)
	job.Attrs["cmd"] = out.cmd
	job.Attrs["jobinstanceid"] = out.instance
	job.Attrs["completed"] = created(now)

	result := out.result
	status, code := jobDone, 0
	if out.fail != nil {
		status, code = jobFailed, out.fail.code
		result = map[string]any{"errorcode": out.fail.code, "errortext": out.fail.text}
	} else if vm, ok := result["virtualmachine"].(map[string]any); ok && vm["id"] != nil {
		// The machine a job reports carries the job that reported it, with a
		// status of 0 whatever the job's own is. A quirk of the management
		// server's serialisation, kept because a client that decodes the result
		// into a typed struct meets these two fields.
		vm["jobid"], vm["jobstatus"] = id, jobPending
	}
	job.Attrs["jobstatus"] = status
	job.Attrs["jobresultcode"] = code
	// The result is a document the job was given at completion, not a live view
	// of anything: it is stored serialised, and the difference is not cosmetic.
	// A machine's view carries its address, and a map holding it in the store
	// reads as a second holder of that address to the invariant sweep (and would
	// to any reader of a snapshot), when the job only remembers what it was told.
	raw, _ := json.Marshal(result)
	job.Attrs["jobresult"] = string(raw)
	p.env.Store.Put(job)
	return id
}

func (p *Pack) queryAsyncJobResult(a url.Values) (map[string]any, *apiError) {
	if a.Get("jobid") == "" {
		return nil, &apiError{status: 431, code: 431, cs: 4350, text: "Expected parameter job id or parameters resource type and resource id"}
	}
	job, ok := p.env.Store.Get(Name, kindJob, a.Get("jobid"))
	if !ok {
		return nil, unknownEntity("jobid", a.Get("jobid"))
	}
	status := resource.Int(job, "jobstatus")
	body := map[string]any{
		"accountid": accountID, "account": accountName, "domainid": domainID, "domainpath": domainName,
		"userid": userID, "cmd": attr(job, "cmd"),
		"jobstatus":     status,
		"jobprocstatus": 0,
		"jobresultcode": resource.Int(job, "jobresultcode"),
		"created":       created(job.Created),
		"jobid":         job.ID,
	}
	if status != jobPending {
		var result map[string]any
		_ = json.Unmarshal([]byte(attr(job, "jobresult")), &result)
		body["jobresulttype"] = "object"
		body["jobresult"] = result
		body["completed"] = attr(job, "completed")
	}
	if id := attr(job, "jobinstanceid"); id != "" {
		body["jobinstancetype"], body["jobinstanceid"] = "VirtualMachine", id
	}
	return body, nil
}
