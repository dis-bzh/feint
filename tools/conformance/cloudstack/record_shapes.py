#!/usr/bin/env python3
"""Turn an oracle recording of the simulator into the shape fixture the pack is tested against.

    record_shapes.py real.json internal/providers/cloudstack/testdata/simulator-4.23.0.0-shapes.json

A shape is the set of field paths of one kind of answer, lists flattened under
"[]". The free-form `details` map is one opaque path: its keys are data.
"""
import json
import sys

if len(sys.argv) != 3:
    sys.exit(__doc__)
real = {s["label"]: s["body"] for s in json.load(open(sys.argv[1]))}
OPAQUE = {"details"}


def paths(v, p=""):
    out = set()
    if isinstance(v, dict):
        for k, x in v.items():
            q = f"{p}.{k}" if p else k
            out.add(q)
            if k not in OPAQUE:
                out |= paths(x, q)
    elif isinstance(v, list):
        for x in v:
            out |= paths(x, p + "[]")
    return out


def first(label, sub):
    return next(iter(real[label].values()))[sub][0]


def job_vm(label):
    return next(iter(real[label].values()))["jobresult"]["virtualmachine"]


def drop(d, *keys):
    return {k: v for k, v in d.items() if k not in keys}


shapes = {
    "zone": ("a zone, as listZones answers it", first("zones", "zone")),
    "serviceoffering": ("a service offering, as listServiceOfferings answers it", first("offerings", "serviceoffering")),
    "template": ("a template, as listTemplates answers it", first("templates", "template")),
    "network": ("a network, as listNetworks answers it", first("networks", "network")),
    "vm.running": ("a running machine in a list", first("vms-by-name", "virtualmachine")),
    "vm.stopped": ("a machine stopped after it ran, in a list (read off the stop job, which carries the same view plus jobid and jobstatus)", drop(job_vm("stop-job"), "jobid", "jobstatus")),
    "vm.stopped_never_run": ("a machine deployed with startvm=false and never started", first("vms2-state", "virtualmachine")),
    "vm.destroyed": ("a machine destroyed after it ran", first("vms-destroyed", "virtualmachine")),
    "vm.destroyed_never_run": ("a machine destroyed without ever having run", first("vms2-destroyed", "virtualmachine")),
    "volume.running": ("the root volume of a machine that has run", first("volumes-running", "volume")),
    "volume.never_run": ("the root volume of a machine that never ran", first("volumes-never-run", "volume")),
    "answer.list_empty": ("a list with nothing in it", real["vms-empty"]),
    "answer.deploy": ("the immediate answer to deployVirtualMachine", real["deploy"]),
    "answer.stop": ("the immediate answer to stop, start and destroy: the jobid alone", real["stop"]),
    "job.deploy": ("queryAsyncJobResult for a finished deploy", real["deploy-job"]),
    "job.expunge": ("queryAsyncJobResult for a finished expunge", real["expunge-job"]),
    "job.failed": ("queryAsyncJobResult for a job that failed", real["start-running-job"]),
    "error.entity": ("an id that names nothing", real["err-unknown-id"]),
    "error.unauthenticated": ("a request without credentials", real["no-auth"]),
    "error.unknown_command": ("a command that does not exist", real["err-unknown-command"]),
}
doc = {
    "source": "apache/cloudstack-simulator, CloudStack 4.23.0.0, zone deployed by setup/dev/basic.cfg",
    "image": "sha256:59db60234cd0ae7be289d79862de5818fbec4ac618d74b3af448efadbed77c95",
    "recorded": "2026-10-18",
    "how": "tools/conformance/cloudstack/oracle.py real, then record_shapes.py; free-form 'details' maps are recorded as one opaque path",
    "shapes": {k: {"what": w, "paths": sorted(paths(v))} for k, (w, v) in shapes.items()},
}
with open(sys.argv[2], "w") as f:
    json.dump(doc, f, indent=1)
for k, v in doc["shapes"].items():
    print(f"{k:24} {len(v['paths'])} paths")
