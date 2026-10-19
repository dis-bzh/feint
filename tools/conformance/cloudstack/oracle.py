#!/usr/bin/env python3
"""Play one scenario against a CloudStack management server and keep every raw answer.

The point is to have two recordings of the same scenario, one from the official
simulator (the oracle) and one from this emulator's pack, and to compare them
(compare.py). The pack was written against the first and is held to it.

    oracle.py real out.json     # the simulator, logged in as admin/password
    oracle.py mine out.json     # feint, started with FEINT_EXPERIMENTAL_PACKS=cloudstack

The simulator is `docker run --network host apache/cloudstack-simulator`, with a
zone deployed by `python3 /root/tools/marvin/marvin/deployDataCenter.py -i
/root/setup/dev/basic.cfg` inside it (docs/cloudstack.md has the steps, including
what it needs to reach Maven Central from behind a proxy).

Environment: CLOUDSTACK_SIM_URL (default http://127.0.0.1:8080/client/api) and
FEINT_URL (default http://127.0.0.1:4599/client/api).
"""
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import http.cookiejar

if len(sys.argv) != 3 or sys.argv[1] not in ("real", "mine"):
    sys.exit(__doc__)
target, out = sys.argv[1], sys.argv[2]

if target == "real":
    base = os.environ.get("CLOUDSTACK_SIM_URL", "http://127.0.0.1:8080/client/api")
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
else:
    base = os.environ.get("FEINT_URL", "http://127.0.0.1:4599/client/api")
    opener = urllib.request.build_opener()


def raw(params):
    req = urllib.request.Request(base, data=urllib.parse.urlencode(params).encode())
    try:
        r = opener.open(req, timeout=120)
        return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


if target == "real":
    _, body = raw({"command": "login", "username": "admin", "password": "password", "domain": "/", "response": "json"})
    auth = {"sessionkey": json.loads(body)["loginresponse"]["sessionkey"]}
else:
    auth = {"apikey": "k", "signature": "s"}

steps = []


def decode(body):
    if body[:1] in (b"{", b"["):
        return json.loads(body)
    return {"_unparsed": body.decode(errors="replace")[:300]}


def call(label, command, **kw):
    params = {"command": command, "response": "json", **auth, **{k: str(v) for k, v in kw.items()}}
    status, body = raw(params)
    steps.append({"label": label, "command": command, "args": kw, "status": status, "body": decode(body)})
    return steps[-1]["body"]


def inner(doc):
    return next(iter(doc.values())) if isinstance(doc, dict) and len(doc) == 1 else doc


def settle(label, ack):
    """Poll an asynchronous command's job to its end, keeping only the final answer."""
    jid = inner(ack).get("jobid")
    if not jid:
        return None
    for _ in range(120):
        job = call(label, "queryAsyncJobResult", jobid=jid)
        if inner(job).get("jobstatus", 0) != 0:
            return inner(job)
        time.sleep(1)
        steps.pop()
    raise SystemExit("job never settled")


zone = inner(call("zones", "listZones"))["zone"][0]["id"]
offering = inner(call("offerings", "listServiceOfferings"))["serviceoffering"][0]["id"]
template = inner(call("templates", "listTemplates", templatefilter="executable"))["template"][0]["id"]
call("networks", "listNetworks")
call("vms-empty", "listVirtualMachines", name="no-such-machine-name")
deployed = dict(zoneid=zone, templateid=template, serviceofferingid=offering)

ack = call("deploy", "deployVirtualMachine", **deployed, name="oracle-vm")
vm = inner(ack)["id"]
settle("deploy-job", ack)
call("vms-by-name", "listVirtualMachines", name="oracle-vm")
call("volumes-running", "listVolumes", virtualmachineid=vm)
settle("start-running-job", call("start-running", "startVirtualMachine", id=vm))
settle("stop-job", call("stop", "stopVirtualMachine", id=vm))
settle("stop-stopped-job", call("stop-stopped", "stopVirtualMachine", id=vm))
call("volumes-stopped", "listVolumes", virtualmachineid=vm)
settle("start-job", call("start", "startVirtualMachine", id=vm))
settle("destroy-job", call("destroy", "destroyVirtualMachine", id=vm))
call("vms-destroyed", "listVirtualMachines", id=vm)
call("volumes-destroyed", "listVolumes", virtualmachineid=vm)
settle("start-destroyed-job", call("start-destroyed", "startVirtualMachine", id=vm))
settle("expunge-job", call("expunge", "destroyVirtualMachine", id=vm, expunge="true"))
call("vms-after-expunge", "listVirtualMachines", id=vm)
call("volumes-after-expunge", "listVolumes", virtualmachineid=vm)
settle("stop-expunged-job", call("stop-expunged", "stopVirtualMachine", id=vm))

# A second machine, deployed stopped: the edge cases that define behaviour.
ack2 = call("deploy-stopped", "deployVirtualMachine", **deployed, name="oracle-vm2", startvm="false")
vm2 = inner(ack2)["id"]
settle("deploy-stopped-job", ack2)
call("vms2-state", "listVirtualMachines", id=vm2)
call("volumes-never-run", "listVolumes", virtualmachineid=vm2)
settle("destroy-stopped-job", call("destroy-stopped", "destroyVirtualMachine", id=vm2))
call("vms2-destroyed", "listVirtualMachines", id=vm2)
settle("stop-destroyed-job", call("stop-destroyed", "stopVirtualMachine", id=vm2))
settle("destroy-destroyed-job", call("destroy-destroyed", "destroyVirtualMachine", id=vm2))
settle("expunge2-job", call("expunge2", "destroyVirtualMachine", id=vm2, expunge="true"))
ack3 = call("deploy-3", "deployVirtualMachine", **deployed, name="oracle-vm3")
vm3 = inner(ack3)["id"]
settle("deploy-3-job", ack3)
settle("expunge-running-job", call("expunge-running", "destroyVirtualMachine", id=vm3, expunge="true"))

# Refusals and the shapes of lists that match nothing.
# Pagination: page and pagesize go together, count is the total and not the size of the
# page, and a page past the end keeps the count and loses its items.
for label, kw in [
    ("page-1", dict(pagesize=1, page=1)), ("page-2", dict(pagesize=1, page=2)), ("page-3", dict(pagesize=1, page=3)),
    ("page-all", dict(pagesize=5, page=1)), ("page-keyword", dict(pagesize=1, page=1, keyword="Medium")),
    ("page-zero", dict(pagesize=1, page=0)), ("pagesize-all", dict(pagesize=-1)),
    ("pagesize-zero", dict(pagesize=0, page=1)),
    ("pagesize-only", dict(pagesize=1)), ("page-only", dict(page=1)), ("pagesize-zero-alone", dict(pagesize=0)),
    ("pagesize-all-page", dict(pagesize=-1, page=1)), ("pagesize-too-big", dict(pagesize=1000, page=1)),
    ("page-text", dict(pagesize=1, page="abc")), ("pagesize-text", dict(pagesize="abc", page=1)),
]:
    call(label, "listServiceOfferings", **kw)
call("zones-page-past", "listZones", pagesize=1, page=2)

# What the server says of its own commands, which is what cmk builds its command set from.
call("apis-zones", "listApis", name="listZones")
call("apis-deploy", "listApis", name="deployVirtualMachine")
call("apis-unknown", "listApis", name="nope")
call("zones-name-miss", "listZones", name="nope")
call("zones-unknown-id", "listZones", id="nope")
call("vms-unknown-id", "listVirtualMachines", id="nope")
call("volumes-unknown-vm", "listVolumes", virtualmachineid="nope")
call("offerings-unknown-id", "listServiceOfferings", id="nope")
call("templates-no-filter", "listTemplates")
call("templates-bad-filter", "listTemplates", templatefilter="bogus")
call("err-unknown-id", "stopVirtualMachine", id="nope")
call("err-missing-param", "deployVirtualMachine")
call("err-bad-zone", "deployVirtualMachine", zoneid="nope", templateid=template, serviceofferingid=offering)
call("deploy-bad-template", "deployVirtualMachine", zoneid=zone, templateid="nope", serviceofferingid=offering)
call("deploy-no-template", "deployVirtualMachine", zoneid=zone, serviceofferingid=offering)
call("deploy-bad-network", "deployVirtualMachine", **deployed, networkids="nope")
call("err-bad-job", "queryAsyncJobResult", jobid="nope")
call("err-no-job", "queryAsyncJobResult")
call("start-no-id", "startVirtualMachine")
call("err-unknown-command", "createUniverse")
status, body = raw({"response": "json", **auth})
steps.append({"label": "no-command", "command": "", "args": {}, "status": status, "body": decode(body)})
status, body = raw({"command": "listZones", "response": "json"})
steps.append({"label": "no-auth", "command": "listZones", "args": {}, "status": status, "body": decode(body)})

with open(out, "w") as f:
    json.dump(steps, f, indent=1)
print(target, len(steps), "steps ->", out)
