package cloudstack

import (
	"net/url"
	"strconv"
	"strings"
)

// The catalogue is fiction, and says so. A real CloudStack publishes whatever
// its operator registered; this one publishes one Basic zone, two offerings and
// one template, with fixed identifiers so a fixture can name them. Basic because
// a Basic zone needs no network to deploy into, which is the shortest honest
// path to a machine, and because it is the zone the official simulator's
// basic.cfg deploys, so every field below was read off a zone of the same kind.
//
// The names are this pack's own and the shapes are CloudStack's: each object
// carries exactly the fields a 4.23.0.0 management server answers for it
// (testdata/simulator-4.23.0.0-shapes.json holds them, and
// TestEveryObjectCarriesExactlyTheFieldsTheSimulatorAnswers compares).
const (
	zoneID            = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c01"
	networkID         = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c02"
	templateID        = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c03"
	smallID           = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c04"
	mediumID          = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c05"
	domainID          = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c06"
	osTypeID          = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c07"
	accountID         = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c08"
	userID            = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c09"
	securityGroupID   = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c0a"
	networkOfferingID = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c0b"
	physicalNetworkID = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c0c"
	hostID            = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c0d"
	imageStoreID      = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c0e"
	primaryStoreID    = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c0f"
	clusterID         = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c12"
	podID             = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c13"
	smallDiskID       = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c10"
	mediumDiskID      = "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c11"
	accountName       = "admin"
	domainName        = "ROOT"
	zoneName          = "feint-zone"
	networkName       = "feint-guest-network"
	templateName      = "feint-debian-12"
	guestCIDR         = "10.1.1.0/24"
	guestGateway      = "10.1.1.1"
	guestMask         = "255.255.255.0"
	// The simulated hypervisor's word for itself. Naming a real one would claim
	// that something starts.
	hypervisor = "Simulator"
)

type offering struct {
	id, name, diskID string
	cpus, mhz        int
	memoryInMB       int
}

var offerings = []offering{
	{smallID, "feint.small", smallDiskID, 1, 1000, 1024},
	{mediumID, "feint.medium", mediumDiskID, 2, 1000, 2048},
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

// knownID refuses an id filter that names nothing the cloud has ever held.
//
// A list is the one place that looks forgiving and is not: asking for an id
// that does not exist is an error, 431, and only an id that existed and is gone
// (an expunged machine) answers an empty list. Measured on 4.23.0.0 for every
// list command, so it is checked here once.
func knownID(a url.Values, held ...string) *apiError {
	id := a.Get("id")
	if id == "" {
		return nil
	}
	for _, h := range held {
		if h == id {
			return nil
		}
	}
	return unknownEntity("id", id)
}

// list is the shape of every list answer: a count and the items under their own
// key, and nothing at all when nothing matched. CloudStack omits both for an empty
// list, and a client that expects an empty array there is wrong against the real
// thing.
//
// It pages the matches first, which is what page and pagesize are for and what the
// contract checks an answer against (internal/contract's PageBound): count is the
// total the filters matched and not the size of the page, and a page past the end
// keeps the count and loses its items.
func list(key string, a url.Values, items []any) (map[string]any, *apiError) {
	page, err := paged(a, len(items))
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return map[string]any{}, nil
	}
	body := map[string]any{"count": len(items)}
	if page.end > page.start {
		body[key] = items[page.start:page.end]
	}
	return body, nil
}

// maxPageSize is the management server's own bound, its default.page.size.
const maxPageSize = 500

type window struct{ start, end int }

// paged resolves page and pagesize against the n items that matched, with every
// rule the server applies, each read off 4.23.0.0 (oracle.py, the page-* steps):
//
//   - page and pagesize come together. One without the other is a 431 naming the
//     missing one, except pagesize=-1, which asks for everything and refuses a page;
//   - page is read before pagesize, so a request wrong in both is refused for page;
//   - a page below 1 is the first;
//   - pagesize=0 is a page of nothing: the count survives and the items do not;
//   - more than 500 is refused.
//
// One thing is not reproduced. pagesize below -1 is answered 530 by the server with
// the text of a SQL syntax error, which is a defect of theirs and not a behaviour
// to copy; this pack refuses it as a value it cannot read.
func paged(a url.Values, n int) (window, *apiError) {
	rawPage, rawSize := a.Get("page"), a.Get("pagesize")
	all := window{0, n}

	var page, size int
	var err error
	if rawPage != "" {
		if page, err = strconv.Atoi(rawPage); err != nil {
			return all, invalidValue(rawPage, "page")
		}
	}
	if rawSize != "" {
		if size, err = strconv.Atoi(rawSize); err != nil {
			return all, invalidValue(rawSize, "pagesize")
		}
	}

	switch {
	case rawPage == "" && rawSize == "":
		return all, nil
	case rawSize == "":
		return all, invalid(`"pagesize" parameter is required when "page" is specified`)
	case size == -1 && rawPage == "":
		return all, nil
	case size == -1:
		return all, &apiError{status: 431, code: 431, cs: 4350, text: "Can't specify page parameter when pagesize is -1 (Unlimited)"}
	case size < 0:
		return all, invalidValue(rawSize, "pagesize")
	case rawPage == "":
		return all, invalid(`"page" parameter is required when "pagesize" is specified`)
	case size > maxPageSize:
		return all, &apiError{status: 431, code: 431, cs: 4350, text: "Page size can't exceed max allowed page size value: " + strconv.Itoa(maxPageSize)}
	}

	if page < 1 {
		page = 1
	}
	start := (page - 1) * size
	if start > n {
		start = n
	}
	end := start + size
	if end > n {
		end = n
	}
	return window{start, end}, nil
}

func (p *Pack) listZones(a url.Values) (map[string]any, *apiError) {
	if err := knownID(a, zoneID); err != nil {
		return nil, err
	}
	var items []any
	if matches(a, zoneID, zoneName) {
		items = append(items, zoneView())
	}
	return list("zone", a, items)
}

func zoneView() map[string]any {
	return map[string]any{
		"id": zoneID, "name": zoneName, "dns1": "8.8.8.8", "internaldns1": "8.8.8.8",
		"networktype": "Basic", "securitygroupsenabled": true, "gputotal": 0, "gpuused": 0,
		"allocationstate": "Enabled", "zonetoken": "feint", "dhcpprovider": "VirtualRouter",
		"localstorageenabled": false, "tags": []any{}, "allowuserspecifyvrmtu": false,
		"routerprivateinterfacemaxmtu": 1500, "routerpublicinterfacemaxmtu": 1500, "type": "Core",
		"isnsxenabled": false, "ismultiarch": false, "asnrange": "", "routedmodeenabled": true,
		"hasannotations": false,
	}
}

func (p *Pack) listServiceOfferings(a url.Values) (map[string]any, *apiError) {
	held := make([]string, 0, len(offerings))
	for _, o := range offerings {
		held = append(held, o.id)
	}
	if err := knownID(a, held...); err != nil {
		return nil, err
	}
	var items []any
	for _, o := range offerings {
		if matches(a, o.id, o.name) {
			items = append(items, p.offeringView(o))
		}
	}
	return list("serviceoffering", a, items)
}

func (p *Pack) offeringView(o offering) map[string]any {
	return map[string]any{
		"id": o.id, "name": o.name, "state": "Active", "displaytext": o.name,
		"cpunumber": o.cpus, "cpuspeed": o.mhz, "memory": o.memoryInMB,
		"created": created(p.epoch), "storagetype": "shared", "provisioningtype": "thin",
		"offerha": false, "limitcpuuse": false, "isvolatile": false, "issystem": false,
		"defaultuse": false, "iscustomized": false, "rootdisksize": 0,
		"dynamicscalingenabled": true, "diskofferingstrictness": false,
		"diskofferingid": o.diskID, "diskofferingname": o.name, "diskofferingdisplaytext": o.name,
		"encryptroot": false, "hasannotations": false,
	}
}

// templateFilters are the values listTemplates accepts for templatefilter, the
// constants of the management server's own enumeration.
var templateFilters = map[string]bool{
	"featured": true, "self": true, "selfexecutable": true,
	"sharedexecutable": true, "executable": true, "community": true,
}

func (p *Pack) listTemplates(a url.Values) (map[string]any, *apiError) {
	if f := a.Get("templatefilter"); !templateFilters[f] {
		// The message is the Java exception's own, which is what the server
		// puts in errortext for an enumeration it cannot parse.
		return nil, invalid("No enum constant com.cloud.template.VirtualMachineTemplate.TemplateFilter." + f)
	}
	if err := knownID(a, templateID); err != nil {
		return nil, err
	}
	var items []any
	if matches(a, templateID, templateName) && (a.Get("zoneid") == "" || a.Get("zoneid") == zoneID) {
		items = append(items, p.templateView())
	}
	return list("template", a, items)
}

func (p *Pack) templateView() map[string]any {
	return map[string]any{
		"id": templateID, "name": templateName, "displaytext": "Debian 12 (feint)", "ispublic": true,
		"created": created(p.epoch), "isready": true, "passwordenabled": false, "format": "VHD",
		"isfeatured": true, "crossZones": true, "ostypeid": osTypeID, "ostypename": "Debian GNU/Linux 12 (64-bit)",
		"account": "system", "zoneid": zoneID, "zonename": zoneName, "status": "Download Complete",
		"size": 2147483648, "physicalsize": 2147483648, "templatetype": "BUILTIN", "hypervisor": hypervisor,
		"domain": domainName, "domainpath": domainName, "domainid": domainID, "isextractable": false,
		"checksum": "", "details": map[string]any{},
		"downloaddetails": []any{
			map[string]any{"downloadState": "DOWNLOADED", "datastore": "nfs://feint/secondary", "datastoreRole": "Image", "downloadPercent": "100", "datastoreId": imageStoreID},
			map[string]any{"downloadState": "DOWNLOADED", "datastore": "PS0", "datastoreRole": "Primary", "downloadPercent": "100", "datastoreId": primaryStoreID},
		},
		"arch": "x86_64", "bits": 0, "sshkeyenabled": false, "isdynamicallyscalable": false,
		"directdownload": false, "deployasis": false, "forcks": false, "requireshvm": false,
		"url": "http://feint.invalid/templates/debian-12.vhd.bz2", "tags": []any{}, "hasannotations": false,
	}
}

func (p *Pack) listNetworks(a url.Values) (map[string]any, *apiError) {
	if err := knownID(a, networkID); err != nil {
		return nil, err
	}
	var items []any
	if matches(a, networkID, networkName) && (a.Get("zoneid") == "" || a.Get("zoneid") == zoneID) {
		items = append(items, p.networkView())
	}
	return list("network", a, items)
}

func (p *Pack) networkView() map[string]any {
	svc := func(name, provider string, caps ...any) any {
		if caps == nil {
			caps = []any{}
		}
		return map[string]any{"name": name, "provider": []any{map[string]any{"name": provider}}, "capability": caps}
	}
	return map[string]any{
		"id": networkID, "name": networkName, "displaytext": networkName, "broadcastdomaintype": "Native",
		"traffictype": "Guest", "gateway": guestGateway, "netmask": guestMask, "cidr": guestCIDR,
		"zoneid": zoneID, "zonename": zoneName, "networkofferingid": networkOfferingID,
		"networkofferingname":         "DefaultSharedNetworkOfferingWithSGService",
		"networkofferingdisplaytext":  "Offering for Shared Security group enabled networks",
		"networkofferingconservemode": true, "networkofferingavailability": "Optional", "issystem": false,
		"state": "Setup", "related": networkID, "broadcasturi": "vlan://untagged", "dns1": "8.8.8.8",
		"type": "Shared", "vlan": "untagged", "acltype": "Domain", "subdomainaccess": true,
		"domainid": domainID, "domain": domainName, "domainpath": domainName,
		"service": []any{
			svc("UserData", "VirtualRouter"),
			svc("Dhcp", "VirtualRouter", map[string]any{"name": "DhcpAccrossMultipleSubnets", "value": "true", "canchooseservicecapability": false}),
			svc("Dns", "VirtualRouter", map[string]any{"name": "AllowDnsSuffixModification", "value": "true", "canchooseservicecapability": false}),
			svc("SecurityGroup", "SecurityGroupProvider"),
		},
		"networkdomain": "feint.invalid", "physicalnetworkid": physicalNetworkID, "restartrequired": false,
		"specifyvlan": true, "specifyipranges": true, "canusefordeploy": true, "ispersistent": false,
		"tags": []any{}, "details": map[string]any{}, "displaynetwork": true, "strechedl2subnet": false,
		"redundantrouter": false, "supportsvmautoscaling": false, "created": created(p.epoch),
		"receivedbytes": 0, "sentbytes": 0, "publicmtu": 1500, "privatemtu": 1500, "hasannotations": false,
	}
}
