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

var vmFields = []string{"id", "name", "displayname", "state", "zoneid", "templateid", "serviceofferingid", "cpunumber", "memory", "nic", "created"}

// created renders a time the way CloudStack does: ISO 8601 with a numeric offset
// and no colon in it.
func created(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05-0700") }

// lockAddresses serialises everything that is read-modify-write over the set of
// machines: the choice of a guest address, and the sequence number a machine is
// given. What is free is computed from what exists, and two concurrent deploys
// without it both find the same address free. Terraform deploys several at a
// time.
func (p *Pack) lockAddresses() func() { return serialise.Lock(Name + "/addresses") }

// nextAddress is the lowest free host address of the guest network.
//
// A machine takes an address the first time it starts and keeps it until it is
// expunged: a machine stopped after it ran, or destroyed, still answers its NIC
// with the address, and only one that never ran has none. Measured on 4.23.0.0,
// and an earlier version of this pack read it the other way round from machines
// that had never started, which is what the shape fixture caught.
func (p *Pack) nextAddress() (string, bool) {
	used := map[string]bool{}
	for _, vm := range p.env.Store.List(kindVM, tenant()) {
		if ip := attr(vm, "ipaddress"); ip != "" {
			used[ip] = true
		}
	}
	for host := 10; host <= 250; host++ {
		ip := fmt.Sprintf("10.1.1.%d", host)
		if !used[ip] {
			return ip, true
		}
	}
	return "", false
}

// nextSequence is the machine's number in the management server's database,
// which names it in error sentences and in its instance name (i-2-<n>-VM). It is
// one past everything ever created, expunged machines included.
func (p *Pack) nextSequence() int {
	return len(p.env.Store.List(kindVM, tenant())) + len(p.env.Store.List(kindGone, tenant())) + 1
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

// vmView is a machine as CloudStack describes it, and what it describes depends
// on its history. A machine that has an address (it has started at least once)
// carries it on its NIC and at the top, with its pool type. A running one adds
// its host and its boot mode. One that never ran has none of those, no
// lastupdated if it is still stopped, and a different details map. All measured
// on 4.23.0.0.
func vmView(vm *resource.Resource) map[string]any {
	o, _ := offeringByID(attr(vm, "serviceofferingid"))
	running := vm.State == stateRunning
	started := vm.Attrs["started"] == true
	ip := attr(vm, "ipaddress")
	nicID := attr(vm, "nicid")

	nic := map[string]any{
		"id": nicID, "networkid": networkID, "networkname": networkName, "traffictype": "Guest",
		"type": "Shared", "isdefault": true, "secondaryip": []any{}, "extradhcpoption": []any{},
		"deviceid": "0", "enabled": true,
	}
	if ip != "" {
		nic["netmask"], nic["gateway"], nic["ipaddress"] = guestMask, guestGateway, ip
		nic["broadcasturi"], nic["macaddress"] = "vlan://untagged", mac(nicID)
	}
	details := map[string]any{}
	switch {
	case running:
	case started:
		details["Message.ReservedCapacityFreed.Flag"] = "false"
	default:
		details["deployvm"] = "true"
	}

	view := map[string]any{
		"id": vm.ID, "name": attr(vm, "name"), "displayname": attr(vm, "displayname"),
		"account": accountName, "userid": userID, "username": accountName,
		"domainid": domainID, "domain": domainName, "domainpath": domainName,
		"created": created(vm.Created), "state": vm.State, "haenable": false,
		"zoneid": zoneID, "zonename": zoneName,
		"templateid": templateID, "templatename": templateName, "templatetype": "BUILTIN", "templateformat": "VHD",
		"templatedisplaytext": "Debian 12 (feint)", "passwordenabled": false,
		"isos": []any{}, "isomaxcount": 1,
		"serviceofferingid": o.id, "serviceofferingname": o.name,
		"cpunumber": o.cpus, "cpuspeed": o.mhz, "memory": o.memoryInMB,
		"guestosid": osTypeID, "rootdeviceid": 0, "rootdevicetype": "ROOT",
		"securitygroup": []any{map[string]any{
			"id": securityGroupID, "name": "default", "description": "Default Security Group", "account": accountName,
			"ingressrule": []any{}, "egressrule": []any{}, "tags": []any{}, "virtualmachineids": []any{},
		}},
		"nic": []any{nic}, "hypervisor": hypervisor, "instancename": instanceName(vm), "details": details,
		"affinitygroup": []any{}, "displayvm": true, "isdynamicallyscalable": false, "deleteprotection": false,
		"ostypeid": osTypeID, "osdisplayname": "Debian GNU/Linux 12 (64-bit)",
		"receivedbytes": 0, "sentbytes": 0, "arch": "x86_64", "tags": []any{}, "hasannotations": false,
	}
	// Absent on one machine only: deployed stopped, never run, never touched.
	if running || started || vm.State == stateDestroyed {
		view["lastupdated"] = created(vm.Updated)
	}
	if ip != "" {
		view["ipaddress"], view["pooltype"] = ip, "NetworkFilesystem"
	}
	if running {
		view["hostid"], view["hostname"], view["hostcontrolstate"] = hostID, "SimulatedAgent."+hostID, "Enabled"
		// The boot mode is reported until the machine is first stopped and not
		// restored by the next start, as 4.23.0.0 does: a restarted machine
		// answers a host and an address and no boot mode.
		if vm.Attrs["stopped"] != true {
			view["bootmode"], view["boottype"] = "legacy", "Bios"
		}
	}
	return view
}

func instanceName(vm *resource.Resource) string {
	return "i-2-" + strconv.Itoa(resource.Int(vm, "seq")) + "-VM"
}

func (p *Pack) listVMs(a url.Values) (map[string]any, *apiError) {
	machines := p.env.Store.List(kindVM, tenant())
	held := make([]string, 0, len(machines))
	for _, vm := range machines {
		held = append(held, vm.ID)
	}
	for _, gone := range p.env.Store.List(kindGone, tenant()) {
		held = append(held, gone.ID)
	}
	if err := knownID(a, held...); err != nil {
		return nil, err
	}

	var items []any
	for _, vm := range machines {
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
	o, ok := offeringByID(a.Get("serviceofferingid"))
	if !ok {
		return outcome{}, unknownEntity("serviceofferingid", a.Get("serviceofferingid"))
	}
	// Not required by the API, which can boot from an ISO, a volume or a
	// snapshot instead; this pack serves templates only. Naming none is the
	// management server's own 530.
	switch t := a.Get("templateid"); {
	case t == "":
		return outcome{}, &apiError{status: 530, code: 530, cs: 4250, text: "Please provide only one of the following parameters - template ID, volume ID or snapshot ID"}
	case t != templateID:
		return outcome{}, unknownEntity("templateid", t)
	}
	// A Basic zone has the one guest network. Naming it is allowed, naming
	// anything else is the entity that does not exist.
	for _, n := range strings.Split(a.Get("networkids"), ",") {
		if n != "" && n != networkID {
			return outcome{}, unknownEntity("networkids", n)
		}
	}
	// A value that is not a boolean is not an error here: the management server
	// reads it as the default, true, and deploys.
	start := true
	if v := a.Get("startvm"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			start = b
		}
	}

	defer p.lockAddresses()()
	id := p.env.NewID()
	ip := ""
	if start {
		var free bool
		if ip, free = p.nextAddress(); !free {
			return outcome{}, &apiError{status: 530, code: 530, text: "Unable to find a free IP address in the guest network"}
		}
	}
	name := a.Get("name")
	if name == "" {
		name = "VM-" + id
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
	vm.Attrs["seq"] = p.nextSequence()
	vm.Attrs["ipaddress"] = ip
	vm.Attrs["nicid"] = p.env.NewID()
	vm.Attrs["volumeid"] = p.env.NewID()
	vm.Attrs["userdata"] = a.Get("userdata")
	vm.Attrs["started"] = start
	p.env.Store.Put(vm)
	return outcome{
		cmd:      "org.apache.cloudstack.api.command.admin.vm.DeployVMCmdByAdmin",
		ack:      map[string]any{"id": id},
		instance: id,
		result:   map[string]any{"virtualmachine": vmView(vm)},
	}, nil
}

// machine resolves the id parameter of a command that acts on one.
//
// It accepts the id of an expunged machine, and says so with gone: the
// management server takes the request and the job fails (see goneFailure). Only
// an id it has never held is refused at the door.
func (p *Pack) machine(a url.Values) (vm *resource.Resource, gone *resource.Resource, err *apiError) {
	id := a.Get("id")
	if vm, ok := p.env.Store.Get(Name, kindVM, id); ok {
		return vm, nil, nil
	}
	if g, ok := p.env.Store.Get(Name, kindGone, id); ok {
		return nil, g, nil
	}
	return nil, nil, unknownEntity("id", id)
}

// goneFailure is how a job ends when its machine was expunged. The number in the
// sentence is the database sequence number, not the uuid, as upstream.
func goneFailure(g *resource.Resource, cmd string) outcome {
	return outcome{
		cmd: cmd, instance: g.ID,
		fail: &jobFailure{code: 431, text: "unable to find a virtual machine with id " + strconv.Itoa(resource.Int(g, "seq"))},
	}
}

var errRefused = errors.New("transition refused")

// transition applies a change to a machine under the store's lock, so a check
// and its write cannot be interleaved: two concurrent starts cannot both find the
// machine stopped and both succeed. change may refuse by returning a failure.
func (p *Pack) transition(id string, change func(vm *resource.Resource) *jobFailure) (map[string]any, *jobFailure) {
	var failed *jobFailure
	var moved *resource.Resource
	err := p.env.Store.Update(Name, kindVM, id, func(vm *resource.Resource) error {
		if failed = change(vm); failed != nil {
			return errRefused
		}
		vm.Updated = p.env.Now()
		moved = vm.Clone()
		return nil
	})
	switch {
	case errors.Is(err, errRefused):
		return nil, failed
	case err != nil:
		return nil, &jobFailure{code: 431, text: "unable to find a virtual machine with id " + id}
	}
	return map[string]any{"virtualmachine": vmView(moved)}, nil
}

func (p *Pack) startVM(a url.Values) (outcome, *apiError) {
	const cmd = "org.apache.cloudstack.api.command.admin.vm.StartVMCmdByAdmin"
	vm, gone, err := p.machine(a)
	if err != nil {
		return outcome{}, err
	}
	if gone != nil {
		return goneFailure(gone, cmd), nil
	}

	defer p.lockAddresses()()
	ip, free := p.nextAddress()
	result, failed := p.transition(vm.ID, func(m *resource.Resource) *jobFailure {
		switch m.State {
		case stateRunning:
			return &jobFailure{code: 431, text: "The virtual machine " + m.ID + " (" + attr(m, "name") + ") is already running"}
		case stateDestroyed:
			// The management server cannot place a destroyed machine, and says so
			// in the orchestrator's words, with its instance name and uuid.
			return &jobFailure{code: 530, text: `Unable to orchestrate the start of VM instance {"instanceName":"` + instanceName(m) + `","uuid":"` + m.ID + `"}.`}
		}
		// The address is taken at the first start and kept after, so a machine
		// that has one does not need a free one.
		if attr(m, "ipaddress") == "" {
			if !free {
				return &jobFailure{code: 530, text: "Unable to find a free IP address in the guest network"}
			}
			m.Attrs["ipaddress"] = ip
		}
		m.State = stateRunning
		m.Attrs["started"] = true
		return nil
	})
	return outcome{cmd: cmd, instance: vm.ID, result: result, fail: failed}, nil
}

func (p *Pack) stopVM(a url.Values) (outcome, *apiError) {
	const cmd = "org.apache.cloudstack.api.command.admin.vm.StopVMCmdByAdmin"
	vm, gone, err := p.machine(a)
	if err != nil {
		return outcome{}, err
	}
	if gone != nil {
		return goneFailure(gone, cmd), nil
	}
	// Stopping a machine that is not running succeeds and changes nothing: the
	// job answers the machine as it is, a destroyed one included.
	result, failed := p.transition(vm.ID, func(m *resource.Resource) *jobFailure {
		if m.State == stateRunning {
			m.State = stateStopped
			m.Attrs["stopped"] = true
		}
		return nil
	})
	return outcome{cmd: cmd, instance: vm.ID, result: result, fail: failed}, nil
}

func (p *Pack) destroyVM(a url.Values) (outcome, *apiError) {
	const cmd = "org.apache.cloudstack.api.command.admin.vm.DestroyVMCmdByAdmin"
	vm, gone, err := p.machine(a)
	if err != nil {
		return outcome{}, err
	}
	if gone != nil {
		return goneFailure(gone, cmd), nil
	}
	// expunge is a boolean the management server parses strictly, and a value
	// that is not one reads as false.
	expunge, _ := strconv.ParseBool(a.Get("expunge"))
	out := outcome{cmd: cmd, instance: vm.ID}

	if expunge {
		defer p.lockAddresses()()
		p.env.Store.Delete(Name, kindVM, vm.ID)
		g := resource.New(vm.ID, kindGone, tenant(), "expunged", p.env.Now())
		g.Attrs["seq"] = resource.Int(vm, "seq")
		g.Attrs["volumeid"] = attr(vm, "volumeid")
		p.env.Store.Put(g)
		// What an expunge answers is a machine with nothing in it, not a
		// success flag: the stub the management server builds once the row is
		// removed. Measured, and not what the documentation says.
		out.result = map[string]any{"virtualmachine": map[string]any{
			"securitygroup": []any{}, "nic": []any{}, "affinitygroup": []any{}, "deleteprotection": false, "tags": []any{},
		}}
		return out, nil
	}
	// Destroyed, not gone: the machine stays listable until it is expunged, and
	// keeps its address until then.
	out.result, out.fail = p.transition(vm.ID, func(m *resource.Resource) *jobFailure {
		m.State = stateDestroyed
		return nil
	})
	return out, nil
}
