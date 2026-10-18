package cloudstack

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/stephrobert/feint/internal/core/resource"
)

// A machine has one volume, its root, and this pack keeps no resource for it: the
// volume is the machine's, its identifier is minted with the machine, and it goes
// when the machine is expunged. Nothing else can attach to it or detach from it
// yet, which is what would make it a resource of its own.
//
// What a volume answers depends on the machine's history, as the machine does.
// One that never ran is Allocated: it has no pool, no pod, no cluster and no I/O
// counters, and says its hypervisor is "None". The first start makes it Ready and
// gives it all of them. Measured on 4.23.0.0.

const volumeSize = 2147483648

// diskOfferingNumber is the number the management server reports as a root
// volume's serviceofferingid: its database sequence, not a uuid, which is an
// oddity of that answer and not a choice made here.
func diskOfferingNumber(o offering) string {
	for i, known := range offerings {
		if known.id == o.id {
			return strconv.Itoa(i + 1)
		}
	}
	return "1"
}

func (p *Pack) volumeView(vm *resource.Resource) map[string]any {
	o, _ := offeringByID(attr(vm, "serviceofferingid"))
	ran := vm.Attrs["started"] == true
	view := map[string]any{
		"id": attr(vm, "volumeid"), "name": "ROOT-" + strconv.Itoa(resource.Int(vm, "seq")),
		"zoneid": zoneID, "zonename": zoneName, "type": "ROOT", "deviceid": 0,
		"virtualmachineid": vm.ID, "templateid": templateID, "templatename": templateName,
		"templatedisplaytext": "Debian 12 (feint)", "vmname": attr(vm, "name"), "vmdisplayname": attr(vm, "displayname"),
		"vmstate": vm.State, "vmtype": "User", "provisioningtype": "thin", "size": volumeSize,
		"created": created(vm.Created), "state": "Allocated",
		"account": accountName, "domainid": domainID, "domain": domainName, "domainpath": domainName,
		"storagetype": "shared", "hypervisor": "None", "destroyed": false,
		"serviceofferingid": diskOfferingNumber(o), "serviceofferingname": o.name, "serviceofferingdisplaytext": o.name,
		"isextractable": false, "displayvolume": true, "quiescevm": false, "supportsstoragesnapshot": false,
		"deleteprotection": false, "tags": []any{}, "hasannotations": false,
	}
	if ran {
		view["state"], view["hypervisor"] = "Ready", hypervisor
		view["diskkbsread"], view["diskkbswrite"], view["diskioread"], view["diskiowrite"] = 0, 0, 0, 0
		view["storage"], view["storageid"], view["path"] = "PS0", primaryStoreID, attr(vm, "volumeid")
		view["clusterid"], view["clustername"] = clusterID, "C0"
		view["podid"], view["podname"] = podID, "POD0"
	}
	return view
}

func (p *Pack) listVolumes(a url.Values) (map[string]any, *apiError) {
	machines := p.env.Store.List(kindVM, tenant())
	gone := p.env.Store.List(kindGone, tenant())

	// Every identifier the cloud has held, so that one it never held is refused
	// and one it held and lost answers an empty list, for both filters.
	var volumes, vms []string
	for _, vm := range machines {
		volumes, vms = append(volumes, attr(vm, "volumeid")), append(vms, vm.ID)
	}
	for _, g := range gone {
		volumes, vms = append(volumes, attr(g, "volumeid")), append(vms, g.ID)
	}
	if err := knownID(a, volumes...); err != nil {
		return nil, err
	}
	if v := a.Get("virtualmachineid"); v != "" {
		known := false
		for _, id := range vms {
			known = known || id == v
		}
		if !known {
			return nil, unknownEntity("virtualmachineid", v)
		}
	}

	var items []any
	for _, vm := range machines {
		view := p.volumeView(vm)
		if !matches(a, attr(vm, "volumeid"), view["name"].(string)) {
			continue
		}
		if v := a.Get("virtualmachineid"); v != "" && v != vm.ID {
			continue
		}
		if v := a.Get("type"); v != "" && !strings.EqualFold(v, "ROOT") {
			continue
		}
		if v := a.Get("zoneid"); v != "" && v != zoneID {
			continue
		}
		items = append(items, view)
	}
	return list("volume", items), nil
}
