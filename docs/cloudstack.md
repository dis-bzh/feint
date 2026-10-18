# CloudStack (prototype)

An experimental fourth pack: Apache CloudStack, without a CloudStack. It is not
part of the product yet. It is off unless asked for, it has no committed contract,
coverage or evidence record, and this page says which of those stand between it
and a default mount.

```bash
FEINT_EXPERIMENTAL_PACKS=cloudstack feint serve
eval "$(FEINT_EXPERIMENTAL_PACKS=cloudstack feint env cloudstack)"
```

`cmk` keeps its endpoint in its own profile, so the environment cannot point it:
`cmk set url http://127.0.0.1:4599/client/api`, then `set apikey feint-api-key`,
`set secretkey feint-secret-key`, `cmk sync`.

## How it was written: against the real server

The pack is held to a CloudStack 4.23.0.0 management server, the official
`apache/cloudstack-simulator` image, with the zone its `basic.cfg` deploys. That
server is the oracle, and the first version of this pack, written from the API's
documentation and from memory, differed from it on 19 of the first 28 recorded
steps (the other 9 were already identical). Four findings that no amount of reading
would have turned up:

- starting a running machine answers **HTTP 200** and a jobid, and the *job* fails
  with code 431; the pack answered 431 at once;
- stopping a machine that is not running **succeeds**, and so does destroying a
  destroyed one;
- `destroyVirtualMachine` with `expunge` answers a machine stub with empty lists,
  not `{"success": true}`;
- a machine keeps its address from its first start until it is expunged. A first
  correction of mine read the opposite, off machines that had never started, and
  the shape fixture below caught the mistake.

What now holds the pack to the server, all of it offline and in `go test`:

| What | Where | Held by |
|---|---|---|
| The field set of 20 kinds of answer (zone, offering, template, network, a machine in each of five histories, a volume in two, three job envelopes, three errors) | `testdata/simulator-4.23.0.0-shapes.json` | `TestEveryObjectCarriesExactlyTheFieldsTheSimulatorAnswers`, in both directions: a field the server answers and the pack omits, and one the pack answers and the server does not |
| The parameters, types and required flags of the 12 served commands | `testdata/simulator-4.23.0.0-listapis.json` | `TestTheServedCommandsDeclareWhatTheServerDeclares` |
| Behaviour: job failures, idempotent stop, address lifetime, expunged rows, boot mode, error sentences | `behaviour_test.go` | each case is one where the first version disagreed |

And the two recordings can be redone and compared whenever a simulator is at hand:

```bash
tools/conformance/cloudstack/oracle.py real real.json     # the simulator
tools/conformance/cloudstack/oracle.py mine mine.json     # this pack
tools/conformance/cloudstack/compare.py real.json mine.json
```

On 2026-10-18 that is 66 steps and **0 differences** in status, field set and error
sentence. It does not compare values, and it can only be as good as its scenario:
a command or a state it never plays is not covered.

### Starting the simulator

```bash
docker run -d --name sim --network host apache/cloudstack-simulator
docker exec sim python3 /root/tools/marvin/marvin/deployDataCenter.py -i /root/setup/dev/basic.cfg
```

The image does not carry the Jetty plugin it starts with, so its first boot needs
Maven Central (about 90 s), and behind a proxy it needs Maven's proxy settings and
the proxy's certificate in a truststore (`-v settings.xml:/root/.m2/settings.xml`
and `-e JAVA_TOOL_OPTIONS=-Djavax.net.ssl.trustStore=...`). Log in as
`admin`/`password` with `domain=/`, or `registerUserKeys` for an API key.

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
configuration is inside `terraform.sh` and not in a `.tf` file beside it for a
reason that is worth knowing: `internal/cli` reads any `.tf` under
`tools/conformance` as a stack CI applies and publishes it as proof, and none is.
It moves to a file the day a workflow runs it.

What the Ansible run exercises is narrower than the collection. `instance` calls
`listVolumes`, `listNetworks`, `listVPCs`, `listPods`, `listHosts` and many more
on paths this playbook does not take (SSH keys, security groups, tags, resizing,
migration); the pack serves none of those, and `ngine_io.cloudstack.instance`
with any of those options will meet an `Unknown API command`.

## What it serves

Twelve commands, each mounted for GET and POST, which is why the server reports 24
routes: `listApis`, `listZones`, `listServiceOfferings`, `listTemplates`,
`listNetworks`, `listVolumes`, `deployVirtualMachine`, `listVirtualMachines`,
`startVirtualMachine`, `stopVirtualMachine`, `destroyVirtualMachine` and
`queryAsyncJobResult`.

`listApis` is generated from the table that mounts the routes (`commands.go`), so
it describes exactly what is served and cannot drift from it. That is what lets
`cmk sync` work.

## The dialect, and the one core change it needed

CloudStack has one URL, `/client/api`, and names the operation in a `command`
parameter, in the query of a GET or the form body of a POST. A route's path could
not say that, so `emulator.Route.Path` now accepts a selector:
`/client/api?command=listZones`. The text before `?` is the pattern mounted; the
key and value after it say which request the route answers. It is the same kind of
extension as `{id}:action` (Exoscale), dialect-neutral, held by
`TestSelectorRoutesAreNamedAndServed`. The form body is read to select and put back
for the handler. It touched `table.go` and one dispatcher in `emulator.go`, and no
existing pack.

## What it does not do

Each of these is a decision, not an oversight, and each costs something:

- **Signatures are not verified.** `apikey` and `signature` must be present, in
  any case, and any value is accepted. Credentials, permissions and expiry are not
  emulated.
- **One tenancy.** Every resource belongs to the `ROOT` domain's `admin` account.
  `domainid`, `account` and `projectid` are accepted by no command, and the
  collection's modules resolve them whenever the playbook names one.
- **The catalogue is fiction.** One Basic zone, two offerings, one template and one
  guest network, with fixed identifiers (`catalog.go`). The names are this pack's
  own and the shapes are the server's. No Advanced zone, VPC, public IP, firewall,
  NAT, security group, snapshot, key pair or tag command exists yet, and a machine
  has only its root volume.
- **Jobs are settled before the first poll.** A client that waits on a pending job
  is waiting for information nothing here produces, the same decision the other
  packs make for lifecycle transitions. The server's own list answers a machine
  that has just been deployed with a `jobid` and `jobstatus` still on it, which
  this pack never does.
- **Nothing runs.** No machine runtime is bound: a machine is a record, and the
  hypervisor reads `Simulator`, which is CloudStack's own word for it. Reaching one
  over SSH is not possible.
- **Always JSON.** `response=xml` is ignored, where a real server defaults to XML
  when `response` is absent. Every client above sends `response=json`.
- **Sequence numbers restart at 1.** The server's `i-2-<n>-VM` and the number in
  its "unable to find a virtual machine with id" sentence are its database
  counter, which keeps growing across runs; this pack's starts again with each
  process.

## What stands between this and a default mount

Five tests in `internal/cli` fail when the pack is mounted by default (run them
with `FEINT_EXPERIMENTAL_PACKS=cloudstack go test ./internal/cli/`), and they are
the list. Each waits on something that does not exist yet:

| Test | Waits on |
|---|---|
| `TestEveryPackRendersItsFaultsInItsOwnDialect` | a contract with an error schema |
| `TestTheCommittedArtefactCarriesWhatThePacksDeclare` | `coverage/cloudstack-coverage.json`, which needs the contract to decide what is served, declined or untriaged |
| `TestEveryMountedOperationHasAnEvidenceRow` | 24 rows in `coverage/evidence.json`, which means the conformance legs run in CI and `mise run evidence:update` |
| `TestStatusCountsWhatAClientDrove` | the same record: a provider with no evidence reads as mounting no route |
| `TestTheAxisTableInTheRouteReferenceIsTheCommittedRecord` | `mise run docs:coverage`, once the record exists |

The contract is the root of all five, and what blocked it, a description of the
API that does not come from this pack, now exists: `listApis` on the simulator
answers 865 commands with every parameter, its type and whether it is required,
and the field list of each response. What is left is the converter from that into
`contract.Doc` (a response is wrapped under `<command>response`, and a list under
the name of its item, which `listApis` does not give and the recordings do), and
the choice of which of the 865 are declined, with a reason each.

Done already: the barrage (`storetest.Sweep` and `GoShapes`, which bites: with the
addressing lock removed it failed 4 runs in 5 on 2026-10-18) and the fault dialect.
`Orphans` and `NoLostUpdate` are named in `notInTheBarrage` with their reasons,
and those reasons expire the day a resource names another or a command writes two
fields of one resource.

## Next, in the order they unlock the rest

1. The `listApis` → `contract.Doc` converter, then the coverage artefact and the
   drift gate over 865 commands.
2. A workflow that runs `cs.sh`, `ansible.sh` and `terraform.sh` and the oracle
   comparison against a simulator service, so the claims above are made by CI
   rather than by this page.
3. The commands `ngine_io.cloudstack.instance` calls on the paths this playbook
   does not take, then domain, account and project, then an Advanced zone with
   networks, public IPs and the firewall.
4. Bind a machine runtime, so a deployed machine boots.
