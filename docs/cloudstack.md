# CloudStack (prototype)

An experimental fourth pack: Apache CloudStack, without a CloudStack. It is not
part of the product yet. It is off unless asked for, it has no contract, no
committed coverage and no evidence record, and this page says which of those
stand between it and a default mount.

```bash
FEINT_EXPERIMENTAL_PACKS=cloudstack feint serve
eval "$(FEINT_EXPERIMENTAL_PACKS=cloudstack feint env cloudstack)"
tools/conformance/cloudstack/cs.sh          # the Python library under ngine_io.cloudstack
```

`cmk` keeps its endpoint in its own profile, so the environment cannot point it:
`cmk set url http://127.0.0.1:4599/client/api`, then `set apikey feint-api-key`,
`set secretkey feint-secret-key`, `cmk sync`.

## What a real client has driven

| Client | Version | How | What it did |
|---|---|---|---|
| `cmk` | 6.6.0 | POST, form body | `sync` (builds its command set from `listApis`), list zones, offerings, templates; deploy, stop, start, destroy, expunge; the refusals of a running start and of an unknown id |
| `cs` (Python) | 5.1.0 | GET, query | the same lifecycle with `fetch_result`, which polls `queryAsyncJobResult` the way `poll_job` does in the collection |

Both were driven on 2026-10-18 against this branch. Neither runs in CI: the
Python one is `tools/conformance/cloudstack/cs.sh`, wired to nothing yet, and
`cmk` was driven by hand. The Ansible modules, the Terraform provider and
`go-cloudstack` have not touched it. Until they have, the claim is "two clients
agree with this pack", which is weaker than "the pack agrees with CloudStack".

## What it serves

Eleven commands: `listApis`, `listZones`, `listServiceOfferings`, `listTemplates`,
`listNetworks`, `deployVirtualMachine`, `listVirtualMachines`,
`startVirtualMachine`, `stopVirtualMachine`, `destroyVirtualMachine` and
`queryAsyncJobResult`. Each is mounted for GET and POST, which is why the server
reports 22 routes.

`listApis` is generated from the same table that mounts the routes
(`commands.go`), so it describes exactly what is served and cannot drift from it.
That is what lets `cmk sync` work, and `TestListApisDescribesExactlyWhatIsServed`
holds it.

## The dialect, and the one core change it needed

CloudStack has one URL, `/client/api`, and names the operation in a `command`
parameter, in the query of a GET or the form body of a POST. A route's path could
not say that, so `emulator.Route.Path` now accepts a selector:
`/client/api?command=listZones`. The text before `?` is the pattern mounted; the
key and value after it say which request the route answers. It is the same kind
of extension as `{id}:action` (Exoscale), dialect-neutral, held by
`TestSelectorRoutesAreNamedAndServed`. The form body is read to select and put
back for the handler. It touched `table.go` and one dispatcher in `emulator.go`,
and no existing pack.

## What it does not do

Each of these is a decision, not an oversight, and each costs something:

- **Signatures are not verified.** `apikey` and `signature` must be present, in
  any case, and any value is accepted. Credentials, permissions and expiry are
  not emulated.
- **One tenancy.** Every resource belongs to the `ROOT` domain's `admin` account.
  `domainid`, `account` and `projectid` are accepted by no command. The Ansible
  collection resolves them constantly, so this is the first thing its modules
  will hit.
- **The catalogue is fiction.** One Basic zone, two offerings, one template and
  one guest network, with fixed identifiers (`catalog.go`). A Basic zone because
  it needs no network to deploy into. No Advanced zone, VPC, public IP, firewall,
  NAT, volume, snapshot, security group, keypair or tag command exists yet.
- **Jobs are settled before the first poll.** A client that waits on a pending
  job is waiting for information nothing here produces, the same decision the
  other packs make for lifecycle transitions.
- **Nothing runs.** No machine runtime is bound: a machine is a record. The
  hypervisor reads `Simulator`, which is CloudStack's own word for its simulated
  one, because naming KVM would claim something starts. Reaching one over SSH is
  not possible.
- **Always JSON.** `response=xml` is ignored, where a real server defaults to XML
  when `response` is absent. Every client above sends `response=json`.
- **Error wording is partly guessed.** The statuses and `errorcode` are
  CloudStack's (401, 431, 432), and the text of an unknown id is the one clients
  match on. The text of a refused transition (starting a running machine) is not
  measured against a real server.

## What stands between this and a default mount

Five tests in `internal/cli` fail when the pack is mounted by default (run them
with `FEINT_EXPERIMENTAL_PACKS=cloudstack go test ./internal/cli/`), and they are
the list. Each waits on something that does not exist yet:

| Test | Waits on |
|---|---|
| `TestEveryPackRendersItsFaultsInItsOwnDialect` | a contract with an error schema: the pack renders faults, but nothing can say its envelope matches the API's |
| `TestTheCommittedArtefactCarriesWhatThePacksDeclare` | `coverage/cloudstack-coverage.json`, which needs the contract to decide what is served, declined or untriaged |
| `TestEveryMountedOperationHasAnEvidenceRow` | 22 rows in `coverage/evidence.json`, which means the conformance legs run in CI and `mise run evidence:update` |
| `TestStatusCountsWhatAClientDrove` | the same record: a provider with no evidence reads as mounting no route |
| `TestTheAxisTableInTheRouteReferenceIsTheCommittedRecord` | `mise run docs:coverage`, once the record exists |

The contract is the root of all five, and fabricating one from this pack's own
`listApis` would be circular: a shape comes from the cloud's own description or
not at all. It has to come from a real management server's `listApis` (the
official simulator image produces one) or from the API annotations, through a
converter into `contract.Doc`.

Done already: the barrage (`storetest.Sweep` and `GoShapes`, which bites: with the
addressing lock removed it fails two runs in three) and the fault dialect.
`Orphans` and `NoLostUpdate` are named in `notInTheBarrage` with their reasons,
and those reasons expire the day a resource names another or a command writes two
fields of one resource.

## Next, in the order they unlock the rest

1. Pull a `listApis` from the official simulator and write the converter; every
   gate above starts from it.
2. Put `cs.sh` and a `cmk` leg under `conformance:leg`, so the driven claim is
   made by CI rather than by this page.
3. Domain, account and project, then an Advanced zone with networks, public IPs
   and the firewall: what `ngine_io.cloudstack`'s `cs_instance` needs to run
   unmodified.
4. Bind a machine runtime, so a deployed machine boots.
