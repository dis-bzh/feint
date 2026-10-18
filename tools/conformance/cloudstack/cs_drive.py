from cs import CloudStack, CloudStackException
import os, sys

cs = CloudStack(endpoint=os.environ["CLOUDSTACK_ENDPOINT"], key=os.environ["CLOUDSTACK_KEY"], secret=os.environ["CLOUDSTACK_SECRET"])

zone = cs.listZones()["zone"][0]
print("zone", zone["name"], zone["networktype"])

# fetch_result=True is what ngine_io.cloudstack's poll_job does by hand: it polls queryAsyncJobResult.
vm = cs.deployVirtualMachine(zoneid=zone["id"],
                             templateid=cs.listTemplates(templatefilter="executable")["template"][0]["id"],
                             serviceofferingid=cs.listServiceOfferings(name="feint.small")["serviceoffering"][0]["id"],
                             name="cs-vm", fetch_result=True)["virtualmachine"]
print("deployed", vm["name"], vm["state"], vm["nic"][0]["ipaddress"])

# what cs_instance does to find "its" machine, and to stay idempotent
found = cs.listVirtualMachines(name="cs-vm")["virtualmachine"]
assert len(found) == 1 and found[0]["id"] == vm["id"], found

print("stopped", cs.stopVirtualMachine(id=vm["id"], fetch_result=True)["virtualmachine"]["state"])
print("started", cs.startVirtualMachine(id=vm["id"], fetch_result=True)["virtualmachine"]["state"])

try:
    cs.startVirtualMachine(id=vm["id"])
except CloudStackException as e:
    print("refused as expected:", str(e)[:90])
else:
    sys.exit("starting a running machine was accepted")

print("expunged", cs.destroyVirtualMachine(id=vm["id"], expunge=True, fetch_result=True))
assert not cs.listVirtualMachines(name="cs-vm"), "machine still listed"
print("clean")
