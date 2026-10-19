# CloudStack (prototype)

An experimental fourth pack: Apache CloudStack, without a CloudStack. It is not
part of the product yet. It is off unless asked for, the tables the README prints
do not count it, and this page says what stands between it and a default mount.

```bash
FEINT_EXPERIMENTAL_PACKS=cloudstack feint serve
eval "$(FEINT_EXPERIMENTAL_PACKS=cloudstack feint env cloudstack)"
```

`cmk` keeps its endpoint in its own profile, so the environment cannot point it:
`cmk set url http://127.0.0.1:4599/client/api`, then `set apikey feint-api-key`,
`set secretkey feint-secret-key`, `cmk sync`.

## How it is held to the real server

The pack is held to a CloudStack 4.23.0.0 management server, the official
`apache/cloudstack-simulator` image, with the zone its `basic.cfg` deploys. That
server is the oracle. The first version of this pack, written from the API's
documentation and from memory, differed from it on 19 of the first 28 recorded
steps, in ways no reading would have turned up: starting a running machine answers
HTTP 200 and the *job* fails; stopping one that is not running succeeds; an expunge
answers a machine stub and not a success flag; a machine keeps its address from its
first start until it is expunged.

`tools/conformance/cloudstack/oracle.py` plays one 85-step scenario against a server
and keeps every raw answer. Played against the simulator, it is
`internal/providers/cloudstack/testdata/simulator-4.23.0.0-recording.json`, and that
file is the one place the pack's tests learn what the real server does:

| What | Held by |
|---|---|
| The fields of 24 kinds of answer (a zone, a machine in each of five histories, a volume in two, three job envelopes, five errors, a page past the end, ...), in both directions: a field the server always answers and the pack omits, one the pack answers and the server never does | `TestEveryObjectCarriesExactlyTheFieldsTheSimulatorAnswers` |
| The recording replayed through the contract: every field the server answered is declared by its own `listApis`, and the types the extractor chose accept what it sent | `TestTheRecordingValidatesAgainstTheContract` |
| The pack's own answers, validated by the observer as they are produced, against the same contract | `TestThePackViolatesNoContractWhilePlayingEveryHistory` |
| The parameters, types, required flags, preferred verb and asynchrony of the 12 served commands, against the contract | `TestTheServedCommandsDeclareWhatTheServerDeclares` |
| Behaviour: job failures, idempotent stop, address lifetime, expunged rows, boot mode, error sentences, pagination | `behaviour_test.go`, one case per place the first version was wrong |

And the live comparison, whenever a simulator is at hand:

```bash
tools/conformance/cloudstack/oracle.py real real.json     # the simulator
tools/conformance/cloudstack/oracle.py mine mine.json     # this pack
tools/conformance/cloudstack/compare.py real.json mine.json tools/conformance/cloudstack/accepted.json
```

On 2026-10-19 that is 85 steps, **0 differences** and 22 accepted ones, each with its
reason in `accepted.json`: the pack does not know which version introduced a command
(`since`) or which commands relate to a parameter, and answers the flat list of
field names a client uses to complete rather than the nested descriptions. A
comparison that accepted nothing would fail on those; one that accepted without a
reason, or that accepted something no longer different, is refused by the tool.
It does not compare values, and it is only as good as its scenario: a command or a
state it never plays is not covered.

### Starting the simulator

```bash
docker run -d --network host apache/cloudstack-simulator
docker exec sim python3 /root/tools/marvin/marvin/deployDataCenter.py -i /root/setup/dev/basic.cfg
```

The image does not carry the Jetty plugin it starts with, so its first boot needs
Maven Central (about 90 s), and behind a proxy it needs Maven's proxy settings and
the proxy's certificate in a truststore (`-v settings.xml:/root/.m2/settings.xml`
and `-e JAVA_TOOL_OPTIONS=-Djavax.net.ssl.trustStore=...`). Log in as
`admin`/`password` with `domain=/`, or `registerUserKeys` for an API key.

## The contract

CloudStack publishes no OpenAPI document. What a management server publishes is
`listApis`: for each of its commands, every parameter (name, type, whether it is
required) and every field of the object the command answers, nested to four levels.
`contracts/cloudstack.json` is that answer, converted by
`tools/contract/extract-cloudstack.py`: 865 commands, 5313 parameters.

`listApis` describes the object a command acts on and says nothing of the wire. Three
things the wire does are written nowhere in it: every answer is wrapped under
`<command>response`; a command that changes something answers a job (`jobid`, and
`id` for the ones that create), not the result; and a list answers `count` and its
items under a key that is the item's own name. So the artefact is honest in two tiers.
**Every** command carries what the server declared of it: its name, its preferred
verb, its parameters, which are required, whether it is asynchronous. A command also
carries the schema of its answer when, and only when, an answer of it has been
recorded (`tools/contract/cloudstack-wrappers.json`, written by
`tools/conformance/cloudstack/record_wrappers.py` from the recording). The 12 served
commands have one. A response schema nobody recorded would be the invented format
this project refuses to ship, and serving one more command is: record it, add it to
the wrappers, regenerate.

Two fields of `contract.Operation` are new, `queryRequired` and `async`, because the
server declares both per command and a `Property` had nowhere to put either.

The schemas are closed on an assumption, as Exoscale's are, and the assumption is
measured: every field the simulator answered for the commands recorded is declared by
its own `listApis`. The one apparent exception, `downloaddetails`, is a list the
server declares without describing its items, which the extractor records as an array
of undescribed items instead of closing it. The error envelope is transcribed from
`ExceptionResponse.java` in `tools/contract/cloudstack-error.json`, which carries the
citation.

Refreshing it needs a simulator, because the source is a running server:

```bash
tools/contract/cloudstack-listapis.sh > .upstream/cloudstack-listapis.json
tools/contract/update.sh          # leaves the artefact alone when that file is absent
```

## The dialect, and the one core change it needed

CloudStack has one URL, `/client/api`, and names the operation in a `command`
parameter, in the query of a GET or the form body of a POST. A route's path could
not say that, so `emulator.Route.Path` now accepts a selector:
`/client/api?command=listZones`. The text before `?` is the pattern mounted; the key
and value after it say which request the route answers. It is the same kind of
extension as `{id}:action` (Exoscale), dialect-neutral, held by
`TestSelectorRoutesAreNamedAndServed`. The form body is read to select and put back
for the handler. It touched `table.go` and one dispatcher in `emulator.go`, and no
existing pack; `probe`, `scw-cli`, `exo-cli` and `terraform` still pass through it.

Each command is mounted for both verbs, because the server takes either and the
clients disagree. The contract names one per command (the verb `listApis` calls its
own: GET for a read, POST for a command that changes something) and that mounting is
the primary one. The other is a second spelling of the same operation, which is what
`Route.Legacy` is for, and it says who sends it. Measured on 2026-10-19: the Python
`cs` library sends every command as a GET, deploy included, and `cmk` posts every
command, a list included. Legacy routes are not counted twice, so the pack mounts 12
operations and `feint serve` reports 24 routes.

## What real clients have driven

Each row was run against this pack **and, as a control, against the official
simulator** with the same script, and gave the same result on both.

| Client | Version | Script | Result |
|---|---|---|---|
| `cmk` | 6.6.0 | by hand | `sync` builds its command set from `listApis`; lists, deploy, stop, start, destroy, expunge, volumes; POST with a form body |
| `cs` (Python) | 5.1.0 | `tools/conformance/cloudstack/cs.sh` | the lifecycle with job polling, a refusal in the job, a refusal at the door; GET |
| `ngine_io.cloudstack` (Ansible) | 3.3.0, the published one | `tools/conformance/cloudstack/ansible.sh` | `zone_info`, `instance` create, create again (changes nothing), `instance_info`, stop, start, expunge: 16 ok, 4 changed, 0 failed, 1 ignored, on both |
| `cloudstack/cloudstack` (Terraform) | 0.7.0, from the registry | `tools/conformance/cloudstack/terraform.sh` | `apply`, an empty second `plan`, `destroy`, on both |

None of the four runs in CI: no workflow calls these scripts. The Terraform
configuration is inside `terraform.sh` and not in a `.tf` file beside it for a reason
worth knowing: `internal/cli` reads any `.tf` under `tools/conformance` as a stack CI
applies and publishes it as proof, and none is. It moves to a file the day a workflow
runs it.

What the Ansible run exercises is narrower than the collection. `instance` calls
`listVolumes`, `listNetworks`, `listVPCs`, `listPods`, `listHosts` and many more on
paths this playbook does not take (SSH keys, security groups, tags, resizing,
migration); the pack serves none of those, and `ngine_io.cloudstack.instance` with
any of those options will meet an `Unknown API command`.

## What it serves

Twelve commands: `listApis`, `listZones`, `listServiceOfferings`, `listTemplates`,
`listNetworks`, `listVolumes`, `deployVirtualMachine`, `listVirtualMachines`,
`startVirtualMachine`, `stopVirtualMachine`, `destroyVirtualMachine` and
`queryAsyncJobResult`. The lists page with `page` and `pagesize` by the server's own
rules: the two go together, `count` is the total and not the page, a page past the
end keeps the count and loses its items, `-1` asks for everything, more than 500 is
refused.

`listApis` is generated from the table that mounts the routes (`commands.go`), so it
describes exactly what is served and cannot drift from it. That is what lets
`cmk sync` work. It says only what is known: an earlier version wrote a `since` of
3.0.0 on every command, which was a figure it made up.

## What it does not serve, and why

`coverage/cloudstack-coverage.json` accounts for all 865 commands:

| | Commands |
|---|---|
| Served | 12 |
| Declined, with a reason each | 600, in 30 groups |
| Backlog (`coverage/cloudstack-baseline.json`) | 253 |

The line between the last two is the one the other packs draw. A command is
declined when it cannot be answered honestly; everything else is backlog, and the
coverage report is the list of it. An object that does not exist here (a host, a
backup, a BGP peer, a GPU) is declined with every command that reads or writes it,
because an empty answer would be true and a successful write would not. An object
that exists as a fixed fiction (a pod, the root domain) has its reads in the backlog.
The reason of each group is in `declined.go`, with what would make it revisitable.

**Declined, by reason.** The groups, largest first:

| Commands | Group | Why |
|---:|---|---|
| 95 | The operator's side of the infrastructure | one fictional zone and nothing beneath it: no pod, cluster, host, physical network or management server to add, change, dedicate or put in maintenance |
| 87 | Access control and identity | every credential is accepted on purpose; users, roles, LDAP, OAuth, two-factor and the lifecycle of accounts and domains would describe an access control nothing applies |
| 65 | Vendor appliances and bare metal | Netscaler, Palo Alto, Nicira, UCS, SolidFire, PXE: devices the emulator does not run |
| 38 | Storage backends and object storage | pools, image stores, selectors and buckets: storage that does not exist |
| 26 | Backup and recovery | a backup copies disks to a repository through a provider; no disk content, no provider |
| 25 | Dynamic routing | BGP peers, AS numbers, routed prefixes: nothing speaks BGP |
| 21 | Kubernetes service | a managed control plane built from machines and an ISO |
| 21 | Autoscaling | decisions taken from load nothing measures |
| 20 | Image bytes | registering, uploading, extracting, copying: bytes the emulator neither holds nor fetches |
| 19 | Migration and taking over | presupposes hosts and a hypervisor |
| 17 | Usage, capacity, metrics | figures the emulator would invent and somebody act on |
| 15 | GPU | hardware of hosts that do not exist |
| 14 | DNS framework | records published through an external provider |
| 14 | CloudStack's own appliance machines | routers and system VMs the emulator does not run |
| 13 | Guest OS tables | translation to a hypervisor that is absent |
| 12 | Extensions and custom actions | scripts run on the management server |
| 12 | Shared file systems | an appliance machine exporting NFS |
| 10 | Webhooks | outgoing HTTP to a URL the client names |
| 10 | Key management and HSM | key material the emulator does not hold |
| 10 | Certificates | for agents and hosts that do not exist |
| 9 | Events and alerts | the server's audit trail, which the emulator does not keep |
| 9 | Global load balancing, portable IPs | span zones; there is one |
| 8 | Schedules | a schedule nothing runs |
| 7 | Themes and icons | the web interface is not served |
| 6 | VNF appliances | vendor network functions the emulator does not run |
| 5 | CloudStack's simulator plugin | scripts a simulated hypervisor this is not |
| 4 | Global settings | configure a process the emulator is not |
| 3 | Diagnostics | run inside system VMs |
| 3 | Resource limits | a quota nothing enforces |
| 2 | Consoles | no machine has a display |

**Backlog**, the 253 that could be served: the machine and volume lifecycle beyond the
12 (reboot, expunge, recover, restore, resize, attach and detach, scale, update),
snapshots as metadata, networks and VPCs, public IPs, firewall, port forwarding and
NAT, load balancers, VPN, security groups, offerings, SSH keys, user data, tags,
affinity and instance groups, projects, annotations, and the reads of the fixed
topology and tenancy (`listAccounts`, `listDomains`, `listPods`, `listHosts`,
`listStoragePools`, `listCapabilities`, `listAsyncJobs`). This is the list that
`ngine_io.cloudstack.instance` walks as it grows.

## What it does not do

Each of these is a decision, not an oversight, and each costs something:

- **Signatures are not verified.** `apikey` and `signature` must be present, in any
  case, and any value is accepted.
- **One tenancy.** Every resource belongs to the `ROOT` domain's `admin` account.
  `domainid`, `account` and `projectid` are accepted by no command.
- **The catalogue is fiction.** One Basic zone, two offerings, one template and one
  guest network, with fixed identifiers (`catalog.go`). The names are this pack's own
  and the shapes are the server's. A machine has only its root volume.
- **Jobs are settled before the first poll.** A client that waits on a pending job is
  waiting for information nothing here produces, the same decision the other packs
  make for lifecycle transitions.
- **Nothing runs.** No machine runtime is bound: a machine is a record, and the
  hypervisor reads `Simulator`, which is CloudStack's own word for it.
- **Always JSON.** `response=xml` is ignored, where a real server defaults to XML when
  `response` is absent. Every client above sends `response=json`.
- **Sequence numbers restart at 1.** The server's `i-2-<n>-VM` and the number in its
  "unable to find a virtual machine with id" sentence are its database counter, which
  keeps growing across runs.
- **`pagesize` below -1.** The server answers 530 with the text of a SQL syntax
  error. That is a defect of theirs; this pack refuses the value as unreadable.

## What stands between this and a default mount

With `FEINT_EXPERIMENTAL_PACKS=cloudstack go test ./internal/cli/` two tests fail,
and they are the list:

| Test | Waits on |
|---|---|
| `TestEveryMountedOperationHasAnEvidenceRow` | 24 rows in `coverage/evidence.json` |
| `TestTheAxisTableInTheRouteReferenceIsTheCommittedRecord` | `mise run docs:coverage`, once the record has them |

The record is regenerated by `mise run evidence:update`, which runs every client's
suite against one fresh emulator, twice, once of them with machines. It is the one
caller allowed to write the file, and refusing to merge a fresh run with a stale
record is the property it exists for, so rows are not added by hand. It needs `scw`,
`octl`, `exo`, Terraform and Incus, and the CloudStack suites above wired into
`mise run conformance` so that the record can see them drive the pack. Neither is
done: the scripts exist and no workflow runs them.

## Next, in the order they unlock the rest

1. Wire `cs.sh`, `ansible.sh` and `terraform.sh` into `mise run conformance` and the
   conformance workflow, with the oracle comparison against a simulator service, so
   the claims above are made by CI rather than by this page. Then `evidence:update`.
2. The commands `ngine_io.cloudstack.instance` calls on the paths this playbook does
   not take, from the backlog, one recorded command at a time.
3. Domain, account and project as a tenancy model, then an Advanced zone with
   networks, public IPs and the firewall.
4. Bind a machine runtime, so a deployed machine boots.
