package cloudstack

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stephrobert/feint/internal/core/resource"
	"github.com/stephrobert/feint/internal/core/serialise"
)

// The states a machine passes through. CloudStack has more (Starting, Stopping,
// Expunging, Error); transitions here are immediate, so none of them is ever
// observable.
const (
	stateRunning   = "Running"
	stateStopped   = "Stopped"
	stateDestroyed = "Destroyed"
)

// hypervisor is what the management server reports for a machine. "Simulator"
// is CloudStack's own value for its simulated hypervisor, and it is the honest
// one: no machine runs here, and naming a real hypervisor would claim one does.
const hypervisor = "Simulator"

var vmFields = []string{"id", "name", "displayname", "state", "zoneid", "templateid", "serviceofferingid", "cpunumber", "memory", "nic", "created"}

// created renders a time the way CloudStack does: ISO 8601 with a numeric offset
// and no colon in it.
func created(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05-0700") }

// lockAddresses serialises the choice of a guest address, which is
// read-modify-write over the store: what is free is computed from what exists,
// and two concurrent deploys without it both find the same address free.
// Terraform deploys several machines at a time.
func (p *Pack) lockAddresses() func() { return serialise.Lock(Name + "/addresses") }

// nextAddress is the lowest free host address of the guest network. It looks at
// destroyed-but-not-expunged machines too, as the real cloud does: the address
// is held until the machine is gone.
func (p *Pack) nextAddress() (string, bool) {
	used := map[string]bool{}
	for _, vm := range p.env.Store.List(kindVM, tenant()) {
		used[attr(vm, "ipaddress")] = true
	}
	for host := 10; host <= 250; host++ {
		ip := fmt.Sprintf("10.1.1.%d", host)
		if !used[ip] {
			return ip, true
		}
	}
	return "", false
}

func attr(r *resource.Resource, key string) string {
	s, _ := r.Attrs[key].(string)
	return s
}

// mac derives a stable hardware address from the identifier it belongs to, in
// the locally administered range.
func mac(id string) string {
	h := strings.ReplaceAll(id, "-", "")
	return fmt.Sprintf("02:00:%s:%s:%s:%s", h[0:2], h[2:4], h[4:6], h[6:8])
}

// vmView is a machine as CloudStack describes it.
func vmView(vm *resource.Resource) map[string]any {
	o, _ := offeringByID(attr(vm, "serviceofferingid"))
	nicID := attr(vm, "nicid")
	view := map[string]any{
		"id": vm.ID, "name": attr(vm, "name"), "displayname": attr(vm, "displayname"),
		"account": accountName, "domain": domainName, "domainid": domainID,
		"created": created(vm.Created), "state": vm.State, "haenable": false,
		"zoneid": zoneID, "zonename": zoneName,
		"templateid": templateID, "templatename": templateName, "templatedisplaytext": "Debian 12 (feint)",
		"passwordenabled": false, "isdynamicallyscalable": false,
		"serviceofferingid": o.id, "serviceofferingname": o.name,
		"cpunumber": o.cpus, "cpuspeed": o.mhz, "memory": o.memoryInMB,
		"hypervisor": hypervisor, "guestosid": "7d1a2c3e-0f4b-4a52-9c1d-5e6f7a8b9c07",
		"affinitygroup": []any{}, "securitygroup": []any{}, "tags": []any{},
		"nic": []any{map[string]any{
			"id": nicID, "networkid": networkID, "networkname": networkName, "netmask": guestMask,
			"gateway": guestGateway, "ipaddress": attr(vm, "ipaddress"), "traffictype": "Guest",
			"type": "Shared", "isdefault": true, "macaddress": mac(nicID),
		}},
	}
	if k := attr(vm, "keypair"); k != "" {
		view["keypair"] = k
	}
	return view
}

func (p *Pack) listVMs(a url.Values) (map[string]any, *apiError) {
	var items []any
	for _, vm := range p.env.Store.List(kindVM, tenant()) {
		if !matches(a, vm.ID, attr(vm, "name")) {
			continue
		}
		if v := a.Get("zoneid"); v != "" && v != zoneID {
			continue
		}
		if v := a.Get("state"); v != "" && !strings.EqualFold(v, vm.State) {
			continue
		}
		items = append(items, vmView(vm))
	}
	return list("virtualmachine", items), nil
}

func (p *Pack) deployVM(a url.Values) (outcome, *apiError) {
	if a.Get("zoneid") != zoneID {
		return outcome{}, unknownEntity("zoneid", a.Get("zoneid"))
	}
	if a.Get("templateid") != templateID {
		return outcome{}, unknownEntity("templateid", a.Get("templateid"))
	}
	o, ok := offeringByID(a.Get("serviceofferingid"))
	if !ok {
		return outcome{}, unknownEntity("serviceofferingid", a.Get("serviceofferingid"))
	}
	// A Basic zone has the one guest network. Naming it is allowed, naming
	// anything else is the entity that does not exist.
	for _, n := range strings.Split(a.Get("networkids"), ",") {
		if n != "" && n != networkID {
			return outcome{}, unknownEntity("networkids", n)
		}
	}
	start := true
	if v := a.Get("startvm"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return outcome{}, invalid("Invalid parameter startvm value=" + v)
		}
		start = b
	}

	defer p.lockAddresses()()
	ip, free := p.nextAddress()
	if !free {
		return outcome{}, &apiError{status: 530, code: 530, text: "Unable to find a free IP address in the guest network"}
	}
	id := p.env.NewID()
	name := a.Get("name")
	if name == "" {
		name = id
	}
	display := a.Get("displayname")
	if display == "" {
		display = name
	}
	state := stateRunning
	if !start {
		state = stateStopped
	}
	vm := resource.New(id, kindVM, tenant(), state, p.env.Now())
	vm.Attrs["name"] = name
	vm.Attrs["displayname"] = display
	vm.Attrs["serviceofferingid"] = o.id
	vm.Attrs["ipaddress"] = ip
	vm.Attrs["nicid"] = p.env.NewID()
	vm.Attrs["keypair"] = a.Get("keypair")
	vm.Attrs["userdata"] = a.Get("userdata")
	p.env.Store.Put(vm)
	return outcome{
		cmd:    "org.apache.cloudstack.api.command.user.vm.DeployVMCmd",
		ack:    map[string]any{"id": id},
		result: map[string]any{"virtualmachine": vmView(vm)},
	}, nil
}

// machine resolves the id parameter of a command that acts on one.
func (p *Pack) machine(a url.Values) (*resource.Resource, *apiError) {
	vm, ok := p.env.Store.Get(Name, kindVM, a.Get("id"))
	if !ok {
		return nil, unknownEntity("id", a.Get("id"))
	}
	return vm, nil
}

// transition moves a machine to a state, and refuses atomically: the check and
// the write happen under the store's lock, so two concurrent starts cannot both
// find the machine stopped and both succeed.
func (p *Pack) transition(id string, refuse func(state string) *apiError, to string) (map[string]any, *apiError) {
	var refused *apiError
	var moved *resource.Resource
	err := p.env.Store.Update(Name, kindVM, id, func(vm *resource.Resource) error {
		if refused = refuse(vm.State); refused != nil {
			return errRefused
		}
		vm.State = to
		vm.Updated = p.env.Now()
		moved = vm.Clone()
		return nil
	})
	switch {
	case errors.Is(err, errRefused):
		return nil, refused
	case err != nil:
		return nil, unknownEntity("id", id)
	}
	return map[string]any{"virtualmachine": vmView(moved)}, nil
}

var errRefused = errors.New("transition refused")

func (p *Pack) startVM(a url.Values) (outcome, *apiError) {
	vm, err := p.machine(a)
	if err != nil {
		return outcome{}, err
	}
	result, err := p.transition(vm.ID, func(state string) *apiError {
		if state != stateStopped {
			return invalid("Unable to start a virtual machine " + vm.ID + " in state " + state)
		}
		return nil
	}, stateRunning)
	if err != nil {
		return outcome{}, err
	}
	return outcome{cmd: "org.apache.cloudstack.api.command.user.vm.StartVMCmd", ack: map[string]any{"id": vm.ID}, result: result}, nil
}

func (p *Pack) stopVM(a url.Values) (outcome, *apiError) {
	vm, err := p.machine(a)
	if err != nil {
		return outcome{}, err
	}
	result, err := p.transition(vm.ID, func(state string) *apiError {
		if state != stateRunning {
			return invalid("Unable to stop a virtual machine " + vm.ID + " in state " + state)
		}
		return nil
	}, stateStopped)
	if err != nil {
		return outcome{}, err
	}
	return outcome{cmd: "org.apache.cloudstack.api.command.user.vm.StopVMCmd", ack: map[string]any{"id": vm.ID}, result: result}, nil
}

func (p *Pack) destroyVM(a url.Values) (outcome, *apiError) {
	vm, err := p.machine(a)
	if err != nil {
		return outcome{}, err
	}
	expunge := false
	if v := a.Get("expunge"); v != "" {
		b, perr := strconv.ParseBool(v)
		if perr != nil {
			return outcome{}, invalid("Invalid parameter expunge value=" + v)
		}
		expunge = b
	}
	out := outcome{
		cmd: "org.apache.cloudstack.api.command.user.vm.DestroyVMCmd",
		ack: map[string]any{"id": vm.ID},
	}
	if expunge {
		p.env.Store.Delete(Name, kindVM, vm.ID)
		out.result = map[string]any{"success": true}
		return out, nil
	}
	// Destroyed, not gone: the machine stays listable until it is expunged, and
	// holds its address, as upstream does for the expunge delay.
	out.result, err = p.transition(vm.ID, func(string) *apiError { return nil }, stateDestroyed)
	if err != nil {
		return outcome{}, err
	}
	return out, nil
}
