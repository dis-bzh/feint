"""Drive a CloudStack endpoint with the Python `cs` library, the client under ngine_io.cloudstack.

It names nothing of the catalogue (first zone, first template, first offering), so
the same script runs against the official simulator and against this emulator, and
a difference between the two is a difference in the pack. Reads CLOUDSTACK_ENDPOINT,
CLOUDSTACK_KEY and CLOUDSTACK_SECRET.
"""
import os
import sys

from cs import CloudStack, CloudStackException

cs = CloudStack(endpoint=os.environ["CLOUDSTACK_ENDPOINT"], key=os.environ["CLOUDSTACK_KEY"], secret=os.environ["CLOUDSTACK_SECRET"])

# One name per run: a machine left by an interrupted earlier run must not be found as this one.
NAME = "cs-vm-%d" % os.getpid()

zone = cs.listZones()["zone"][0]
template = cs.listTemplates(templatefilter="executable")["template"][0]["id"]
offering = cs.listServiceOfferings()["serviceoffering"][0]["id"]
print("zone", zone["networktype"])

# fetch_result=True is what ngine_io.cloudstack's poll_job does by hand: it polls queryAsyncJobResult.
vm = cs.deployVirtualMachine(zoneid=zone["id"], templateid=template, serviceofferingid=offering, name=NAME, fetch_result=True)["virtualmachine"]
print("deployed", vm["state"], bool(vm["nic"][0].get("ipaddress")))

# what cs_instance does to find "its" machine, and to stay idempotent
found = cs.listVirtualMachines(name=NAME)["virtualmachine"]
assert len(found) == 1 and found[0]["id"] == vm["id"], found

# a root volume that follows the machine, which is what cs_instance reads to size it
volume = cs.listVolumes(virtualmachineid=vm["id"], type="ROOT")["volume"]
assert len(volume) == 1 and volume[0]["state"] == "Ready" and volume[0]["size"] >> 30 == 2, volume

print("stopped", cs.stopVirtualMachine(id=vm["id"], fetch_result=True)["virtualmachine"]["state"])
print("stopped again", cs.stopVirtualMachine(id=vm["id"], fetch_result=True)["virtualmachine"]["state"])
print("started", cs.startVirtualMachine(id=vm["id"], fetch_result=True)["virtualmachine"]["state"])

# The command is accepted (HTTP 200, a jobid) and the JOB fails: a client that waits
# on it is the one that sees the refusal, which `cs` raises as an exception.
try:
    cs.startVirtualMachine(id=vm["id"], fetch_result=True)
except CloudStackException as e:
    print("refused in the job:", "already running" in str(e))
else:
    sys.exit("starting a running machine succeeded")

try:
    cs.stopVirtualMachine(id="not-a-machine")
except CloudStackException as e:
    print("refused at the door:", "Invalid parameter id value=not-a-machine" in str(e))
else:
    sys.exit("an unknown id was accepted")

cs.destroyVirtualMachine(id=vm["id"], expunge=True, fetch_result=True)
assert not cs.listVirtualMachines(name=NAME), "machine still listed"
print("clean")
