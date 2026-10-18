package cloudstack

import (
	"net/url"
	"strings"
)

// The catalogue is fiction, and says so. A real CloudStack publishes whatever
// its operator registered; this one publishes one Basic zone, two offerings and
// one template, with fixed identifiers so a fixture can name them. Basic rather
// than Advanced because a Basic zone needs no network to deploy into, which is
// the shortest honest path to a machine.
const (
	zoneID       = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c01"
	networkID    = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c02"
	templateID   = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c03"
	smallID      = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c04"
	mediumID     = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c05"
	domainID     = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c06"
	accountName  = "admin"
	domainName   = "ROOT"
	zoneName     = "feint-zone"
	networkName  = "feint-guest-network"
	templateName = "feint-debian-12"
	guestCIDR    = "10.1.1.0/24"
	guestGateway = "10.1.1.1"
	guestMask    = "255.255.255.0"
)

type offering struct {
	id, name   string
	cpus, mhz  int
	memoryInMB int
}

var offerings = []offering{
	{smallID, "feint.small", 1, 1000, 1024},
	{mediumID, "feint.medium", 2, 1000, 2048},
}

func offeringByID(id string) (offering, bool) {
	for _, o := range offerings {
		if o.id == id {
			return o, true
		}
	}
	return offering{}, false
}

// matches applies the filters every list command shares: id, name and keyword.
func matches(a url.Values, id, name string) bool {
	if v := a.Get("id"); v != "" && v != id {
		return false
	}
	if v := a.Get("name"); v != "" && !strings.EqualFold(v, name) {
		return false
	}
	if v := a.Get("keyword"); v != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(v)) {
		return false
	}
	return true
}

// list is the shape of every list answer: a count and the items under their own
// key, and nothing at all when there are none. CloudStack omits both for an empty
// list, and a client that expects an empty array there is wrong against the real
// thing.
func list(key string, items []any) map[string]any {
	if len(items) == 0 {
		return map[string]any{}
	}
	return map[string]any{"count": len(items), key: items}
}

func (p *Pack) listZones(a url.Values) (map[string]any, *apiError) {
	var items []any
	if matches(a, zoneID, zoneName) {
		items = append(items, map[string]any{
			"id": zoneID, "name": zoneName, "networktype": "Basic", "allocationstate": "Enabled",
			"securitygroupsenabled": true, "localstorageenabled": false, "dhcpprovider": "VirtualRouter",
			"zonetoken": "feint", "tags": []any{},
		})
	}
	return list("zone", items), nil
}

func (p *Pack) listServiceOfferings(a url.Values) (map[string]any, *apiError) {
	var items []any
	for _, o := range offerings {
		if !matches(a, o.id, o.name) {
			continue
		}
		items = append(items, map[string]any{
			"id": o.id, "name": o.name, "displaytext": o.name, "cpunumber": o.cpus,
			"cpuspeed": o.mhz, "memory": o.memoryInMB, "iscustomized": false,
			"storagetype": "shared", "provisioningtype": "thin", "created": created(p.env.Now()),
		})
	}
	return list("serviceoffering", items), nil
}

func (p *Pack) listTemplates(a url.Values) (map[string]any, *apiError) {
	var items []any
	if matches(a, templateID, templateName) && (a.Get("zoneid") == "" || a.Get("zoneid") == zoneID) {
		items = append(items, map[string]any{
			"id": templateID, "name": templateName, "displaytext": "Debian 12 (feint)",
			"ostypeid": "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c07", "ostypename": "Debian GNU/Linux 12 (64-bit)",
			"isready": true, "ispublic": true, "isfeatured": true, "passwordenabled": false,
			"format": "QCOW2", "hypervisor": hypervisor, "zoneid": zoneID, "zonename": zoneName,
			"account": accountName, "domain": domainName, "domainid": domainID,
			"templatetype": "USER", "status": "Download Complete", "size": 2147483648,
			"created": created(p.env.Now()), "tags": []any{},
		})
	}
	return list("template", items), nil
}

func (p *Pack) listNetworks(a url.Values) (map[string]any, *apiError) {
	var items []any
	if matches(a, networkID, networkName) && (a.Get("zoneid") == "" || a.Get("zoneid") == zoneID) {
		items = append(items, networkView())
	}
	return list("network", items), nil
}

func networkView() map[string]any {
	return map[string]any{
		"id": networkID, "name": networkName, "displaytext": networkName, "type": "Shared",
		"traffictype": "Guest", "zoneid": zoneID, "zonename": zoneName, "cidr": guestCIDR,
		"gateway": guestGateway, "netmask": guestMask, "state": "Setup", "broadcastdomaintype": "Native",
		"account": accountName, "domain": domainName, "domainid": domainID, "tags": []any{},
		"issystem": false, "canusefordeploy": true,
	}
}
