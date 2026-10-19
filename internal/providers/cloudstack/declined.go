package cloudstack

import (
	"slices"

	"github.com/stephrobert/feint/internal/core/emulator"
)

// because declines commands by their upstream name, spelled the way a route spells its
// operation, so that a decline and a route cannot disagree about what a name is.
func because(reason string, commands ...string) []emulator.Decline {
	operations := make([]string, 0, len(commands))
	for _, c := range commands {
		operations = append(operations, operation(c))
	}
	return emulator.Because(reason, operations...)
}

// Declined implements emulator.Pack.
//
// Only what cannot be answered honestly is here. Everything else the scan reports as
// unknown is backlog, and coverage/cloudstack-baseline.json is the list of it: 253
// commands that could be served, and are not yet.
//
// The line between the two is drawn once, and drawn the same way as for Outscale and
// Exoscale. An object that does not exist here (a host, a backup, a BGP peer, a GPU)
// is declined with every command that reads or writes it, because an empty answer
// would be true and a successful write would not. An object that does exist as a
// fixed fiction (a pod, a cluster, the root domain) has its reads in the backlog and
// its writes declined or backlogged by whether a record would be an honest answer.
//
// The names are exact because they are checked: the drift report calls out an entry
// matching nothing upstream as an orphan.
func (p *Pack) Declined() []emulator.Decline {
	return slices.Concat(
		// Access control and identity.
		//
		// The emulator accepts every credential on purpose (SECURITY.md says so, and docs/roadmap.md lists verifying
		// signatures under not planned), so serving users, roles, accounts, domains' lifecycle, API keys, LDAP, OAuth or
		// two-factor would describe an access control that nothing here applies: a client could read back a role that
		// denies an action and then watch the emulator perform it. That gap is worse than a 404, because a 404 is visible.
		//
		// login is the sharpest case of the same reasoning, as Outscale's CheckAuthentication is: it exists to answer
		// whether a password is right, and the only honest answers here are yes to everything, a verdict that verifies
		// nothing, or an invented refusal.
		//
		// What is NOT here, and is backlog: listAccounts, listDomains, listDomainChildren and the project lifecycle. A
		// client resolves `account=`, `domain=` and `project=` through them (ngine_io.cloudstack does on every module that
		// takes one), so they are served as the fixed tenancy the pack has, not refused. Their writes are here because the
		// emulator has one implicit account.
		because("the emulator accepts every credential on purpose, so serving users, roles, keys, LDAP, OAuth, two-factor or the lifecycle of accounts and domains would describe an access control that nothing here applies",
			"addAccountToProject",
			"addLdapConfiguration",
			"addUserToProject",
			"createAccount",
			"createDomain",
			"createNetworkPermissions",
			"createProjectRole",
			"createProjectRolePermission",
			"createRole",
			"createRolePermission",
			"createUser",
			"deleteAccount",
			"deleteAccountFromProject",
			"deleteDomain",
			"deleteLdapConfiguration",
			"deleteOauthProvider",
			"deleteProjectInvitation",
			"deleteProjectRole",
			"deleteProjectRolePermission",
			"deleteRole",
			"deleteRolePermission",
			"deleteUser",
			"deleteUserFromProject",
			"deleteUserKeys",
			"disableAccount",
			"disableRole",
			"disableUser",
			"enableAccount",
			"enableRole",
			"enableUser",
			"getApiLimit",
			"getCloudIdentifier",
			"getUser",
			"getUserKeys",
			"importLdapUsers",
			"importRole",
			"isAccountAllowedToCreateOfferingsWithTags",
			"ldapConfig",
			"ldapCreateAccount",
			"ldapRemove",
			"linkAccountToLdap",
			"linkDomainToLdap",
			"listIsoPermissions",
			"listLdapConfigurations",
			"listLdapUsers",
			"listNetworkPermissions",
			"listOauthProvider",
			"listProjectAccounts",
			"listProjectInvitations",
			"listProjectRolePermissions",
			"listProjectRoles",
			"listRolePermissions",
			"listRoles",
			"listTemplatePermissions",
			"listUserKeyRules",
			"listUserKeys",
			"listUserTwoFactorAuthenticatorProviders",
			"listUsers",
			"lockAccount",
			"lockUser",
			"login",
			"logout",
			"markDefaultZoneForAccount",
			"moveDomain",
			"moveUser",
			"oauthlogin",
			"registerOauthProvider",
			"registerUserKeys",
			"removeNetworkPermissions",
			"resetApiLimit",
			"resetNetworkPermissions",
			"searchLdap",
			"setupUserTwoFactorAuthentication",
			"unlinkDomainFromLdap",
			"updateAccount",
			"updateDomain",
			"updateIsoPermissions",
			"updateOauthProvider",
			"updateProjectInvitation",
			"updateProjectRole",
			"updateProjectRolePermission",
			"updateRole",
			"updateRolePermission",
			"updateTemplatePermissions",
			"updateUser",
			"validateUserTwoFactorAuthenticationCode",
			"verifyOAuthCodeAndGetUser"),

		// Certificates.
		//
		// These certify the management server to its hosts and agents, or let hosts trust a template server for direct
		// download. The emulator runs no agent, has no host and terminates no TLS of its own, so a certificate issued or
		// revoked here would certify nothing and could be read back as if it did.
		because("certificates authenticate the management server to its hosts and agents, or let hosts trust a template server, and the emulator runs no host, no agent and no TLS, so one issued or revoked here would certify nothing",
			"issueCertificate",
			"listCAProviders",
			"listCaCertificate",
			"listTemplateDirectDownloadCertificates",
			"provisionCertificate",
			"provisionTemplateDirectDownloadCertificate",
			"revokeCertificate",
			"revokeTemplateDirectDownloadCertificate",
			"uploadCustomCertificate",
			"uploadTemplateDirectDownloadCertificate"),

		// Global settings.
		//
		// They configure the management server process, which the emulator is not. A value set here would be read back and
		// change nothing.
		because("global settings configure the management server process, which the emulator is not, so a value set here would be read back and change nothing",
			"listConfigurationGroups",
			"listConfigurations",
			"resetConfiguration",
			"updateConfiguration"),

		// Resource limits.
		//
		// A limit describes a quota that nothing enforces: it would be read back and never applied.
		because("resource limits and counts describe a quota that nothing here enforces, so a limit set would be read back and never applied",
			"listResourceLimits",
			"updateResourceCount",
			"updateResourceLimit"),

		// Events and alerts.
		//
		// They are the management server's own audit trail and health reports, which the emulator does not keep. An empty
		// one would claim that nothing was ever done, and Outscale declines ReadApiLogs for the same reason.
		because("events and alerts are the management server's own audit trail and health reports, which the emulator does not keep, and an empty one would claim that nothing was ever done",
			"archiveAlerts",
			"archiveEvents",
			"deleteAlerts",
			"deleteEvents",
			"generateAlert",
			"listAlertTypes",
			"listAlerts",
			"listEventTypes",
			"listEvents"),

		// Usage, capacity and metrics.
		//
		// These are measurements of consumption and load that nothing here produces, so an answer would be figures the
		// emulator invented and somebody acted on. Outscale declines its consumption and unit price for exactly that
		// reason.
		because("usage records, capacity and metrics are measurements of consumption and load that nothing here produces, so an answer would be figures the emulator invented and somebody acted on",
			"generateUsageRecords",
			"listCapacity",
			"listClustersMetrics",
			"listDbMetrics",
			"listHostsMetrics",
			"listManagementServersMetrics",
			"listStoragePoolsMetrics",
			"listSystemVmsUsageHistory",
			"listUsageRecords",
			"listUsageServerMetrics",
			"listUsageTypes",
			"listVirtualMachinesMetrics",
			"listVirtualMachinesUsageHistory",
			"listVolumesMetrics",
			"listVolumesUsageHistory",
			"listZonesMetrics",
			"removeRawUsageRecords"),

		// CloudStack's simulator plugin.
		//
		// These commands script the simulated hypervisor of CloudStack's own simulator. This emulator is not that plugin,
		// so there is no mock to configure or query.
		because("commands of CloudStack's simulator plugin that script its own simulated hypervisor, and this emulator is not that plugin, so there is no mock to configure or query",
			"cleanupSimulatorMock",
			"configureSimulator",
			"configureSimulatorHAProviderState",
			"listSimulatorHAStateTransitions",
			"querySimulatorMock"),

		// The operator's side of the infrastructure.
		//
		// The emulator has one fictional zone and nothing beneath it: no pod, cluster, host, physical network, traffic
		// type or management server exists to add, change, dedicate, put in maintenance, reach out of band or give high
		// availability. An accepted write would describe hardware that is not there.
		//
		// Their READS are not here: listPods, listClusters, listHosts, listStoragePools, listPhysicalNetworks,
		// listSystemVms and the rest of the topology are backlog, because a fixed fiction can answer them honestly. The
		// reads of the features declined below, dedication, high availability and DRS, are, because the objects they list
		// cannot exist.
		because("the emulator has one fictional zone and nothing beneath it: no pod, cluster, host, physical network or management server exists to add, change, dedicate or put in maintenance, and an accepted write would describe hardware that is not there",
			"acquirePodIpAddress",
			"addCluster",
			"addHost",
			"addNetworkServiceProvider",
			"addRegion",
			"addTrafficType",
			"cancelHostAsDegraded",
			"cancelHostMaintenance",
			"cancelMaintenance",
			"cancelShutdown",
			"changeOutOfBandManagementPassword",
			"changeServiceForRouter",
			"changeServiceForSystemVm",
			"configureHAForHost",
			"configureOutOfBandManagement",
			"createManagementNetworkIpRange",
			"createPhysicalNetwork",
			"createPod",
			"createStorageNetworkIpRange",
			"createVlanIpRange",
			"createZone",
			"declareHostAsDegraded",
			"dedicateCluster",
			"dedicateGuestVlanRange",
			"dedicateHost",
			"dedicatePod",
			"dedicatePublicIpRange",
			"dedicateZone",
			"deleteCluster",
			"deleteHost",
			"deleteManagementNetworkIpRange",
			"deleteNetworkServiceProvider",
			"deletePhysicalNetwork",
			"deletePod",
			"deleteStorageNetworkIpRange",
			"deleteTrafficType",
			"deleteVlanIpRange",
			"deleteZone",
			"disableHAForCluster",
			"disableHAForHost",
			"disableHAForZone",
			"disableOutOfBandManagementForCluster",
			"disableOutOfBandManagementForHost",
			"disableOutOfBandManagementForZone",
			"enableHAForCluster",
			"enableHAForHost",
			"enableHAForZone",
			"enableOutOfBandManagementForCluster",
			"enableOutOfBandManagementForHost",
			"enableOutOfBandManagementForZone",
			"executeClusterDrsPlan",
			"generateClusterDrsPlan",
			"issueOutOfBandManagementPowerAction",
			"listClusterDrsPlan",
			"listDedicatedClusters",
			"listDedicatedGuestVlanRanges",
			"listDedicatedHosts",
			"listDedicatedPods",
			"listDedicatedZones",
			"listHostHAProviders",
			"listHostHAResources",
			"listStorageNetworkIpRange",
			"patchSystemVm",
			"prepareForMaintenance",
			"prepareForShutdown",
			"prepareHostForMaintenance",
			"purgeExpungedResources",
			"readyForShutdown",
			"reconnectHost",
			"releaseDedicatedCluster",
			"releaseDedicatedGuestVlanRange",
			"releaseDedicatedHost",
			"releaseDedicatedPod",
			"releaseDedicatedZone",
			"releaseHostReservation",
			"releasePodIpAddress",
			"releasePublicIpRange",
			"removeManagementServer",
			"removeRegion",
			"scaleSystemVm",
			"startRollingMaintenance",
			"triggerShutdown",
			"updateCluster",
			"updateHost",
			"updateHostPassword",
			"updateNetworkServiceProvider",
			"updatePhysicalNetwork",
			"updatePod",
			"updatePodManagementNetworkIpRange",
			"updateRegion",
			"updateStorageNetworkIpRange",
			"updateTrafficType",
			"updateVlanIpRange",
			"updateZone",
			"upgradeRouterTemplate"),

		// Storage backends and object storage.
		//
		// Primary storage pools, image stores, secondary staging stores, selectors and object stores are the operator's
		// storage backends, and the emulator has none: nothing is stored on one, so registering, migrating or putting one
		// in maintenance would describe storage that does not exist. Buckets are Object Storage, which is not emulated;
		// Outscale declines its export tasks for the same reason. checkVolume and the path and IQN queries ask the backend
		// where it keeps a volume.
		because("storage pools, image stores, object stores and their selectors are the operator's storage backends and the emulator has none: nothing is stored on one, so registering, migrating or maintaining one would describe storage that does not exist",
			"addImageStore",
			"addImageStoreS3",
			"addObjectStoragePool",
			"addSecondaryStorage",
			"addSwift",
			"cancelStorageMaintenance",
			"changeStoragePoolScope",
			"checkVolume",
			"configureStorageAccess",
			"createBucket",
			"createSecondaryStagingStore",
			"createSecondaryStorageSelector",
			"createStoragePool",
			"deleteBucket",
			"deleteImageStore",
			"deleteObjectStoragePool",
			"deleteSecondaryStagingStore",
			"deleteStoragePool",
			"downloadImageStoreObject",
			"enableStorageMaintenance",
			"getPathForVolume",
			"getVolumeSnapshotDetails",
			"getVolumeiScsiName",
			"listBuckets",
			"listImageStoreObjects",
			"listObjectStoragePools",
			"listStoragePoolObjects",
			"migrateResourceToAnotherSecondaryStorage",
			"migrateSecondaryStorageData",
			"removeSecondaryStorageSelector",
			"syncStoragePool",
			"updateBucket",
			"updateCloudToUseObjectStore",
			"updateImageStore",
			"updateObjectStoragePool",
			"updateSecondaryStorageSelector",
			"updateStorageCapabilities",
			"updateStoragePool"),

		// Migration and taking over from a hypervisor.
		//
		// Moving a machine, a volume or a network between hosts and pools, or taking over a machine a hypervisor already
		// runs (unmanaged instances, VMware import), presupposes hosts and a hypervisor the emulator does not have. A
		// migration it accepted would be a placement nobody can observe, and an import would adopt a machine nobody runs.
		because("moving a machine, volume or network between hosts or pools, or taking one over from a hypervisor, presupposes hosts and a hypervisor the emulator does not have, so an accepted migration would be a placement nobody can observe",
			"cleanVMReservations",
			"findHostsForMigration",
			"findStoragePoolsForMigration",
			"importUnmanagedInstance",
			"importVm",
			"importVolume",
			"listAffectedVmsForStorageScopeChange",
			"listImportVmTasks",
			"listUnmanagedInstances",
			"listVmsForImport",
			"listVolumesForImport",
			"migrateNetwork",
			"migrateSystemVm",
			"migrateVPC",
			"migrateVirtualMachine",
			"migrateVirtualMachineWithVolume",
			"migrateVolume",
			"unmanageVirtualMachine",
			"unmanageVolume"),

		// Guest OS tables and hypervisor capabilities.
		//
		// These translate a guest operating system to a hypervisor's own identifier and bound what a hypervisor supports.
		// With no hypervisor there is nothing to translate to. listOsTypes, listOsCategories and listHypervisors are
		// backlog: the fixed catalogue answers them.
		because("guest OS mappings and hypervisor capabilities translate a guest OS to a hypervisor's own identifier, and with no hypervisor there is nothing to translate to",
			"addGuestOs",
			"addGuestOsMapping",
			"addOsCategory",
			"deleteOsCategory",
			"getHypervisorGuestOsNames",
			"listGuestOsMapping",
			"listHypervisorCapabilities",
			"removeGuestOs",
			"removeGuestOsMapping",
			"updateGuestOs",
			"updateGuestOsMapping",
			"updateHypervisorCapabilities",
			"updateOsCategory"),

		// CloudStack's own appliance machines.
		//
		// Virtual routers, system VMs and internal load balancer VMs are machines CloudStack runs for itself. The emulator
		// runs none, so starting, stopping or rebooting one would act on nothing. Listing them is backlog: an empty answer
		// is honest.
		because("routers, system VMs and internal load balancer VMs are CloudStack's own appliance machines, which the emulator does not run, so starting, stopping or rebooting one would act on nothing",
			"configureInternalLoadBalancerElement",
			"configureVirtualRouterElement",
			"createInternalLoadBalancerElement",
			"createVirtualRouterElement",
			"destroyRouter",
			"destroySystemVm",
			"rebootRouter",
			"rebootSystemVm",
			"startInternalLoadBalancerVM",
			"startRouter",
			"startSystemVm",
			"stopInternalLoadBalancerVM",
			"stopRouter",
			"stopSystemVm"),

		// Diagnostics.
		//
		// They run commands inside system VMs and read their health checks, and there are none.
		because("diagnostics run commands inside system VMs and read their health checks, and the emulator runs no system VM",
			"getDiagnosticsData",
			"getRouterHealthCheckResults",
			"runDiagnostics"),

		// Consoles.
		//
		// A console endpoint hands out a websocket to a machine's display, and no machine here has one.
		because("a console endpoint hands out a websocket to a machine's display and no machine here has a display",
			"createConsoleEndpoint",
			"listConsoleSessions"),

		// GPU cards, devices and vGPU profiles.
		//
		// These describe the hardware of hosts, and the emulator has no host.
		because("GPU cards, devices and vGPU profiles are properties of the hardware of hosts, and the emulator has no host",
			"createGpuCard",
			"createGpuDevice",
			"createVgpuProfile",
			"deleteGpuCard",
			"deleteGpuDevice",
			"deleteVgpuProfile",
			"discoverGpuDevices",
			"listGpuCards",
			"listGpuDevices",
			"listVgpuProfiles",
			"manageGpuDevice",
			"unmanageGpuDevice",
			"updateGpuCard",
			"updateGpuDevice",
			"updateVgpuProfile"),

		// Dynamic routing: BGP peers, AS numbers, routed subnets.
		//
		// Routed networking peers with the physical fabric over BGP and hands out IPv4 and IPv6 prefixes from it. Nothing
		// here speaks BGP or owns a fabric, so a peer created would be read back and never peer.
		because("BGP peers, AS numbers and routed prefixes configure dynamic routing with a physical fabric, and nothing here speaks BGP or owns a fabric, so a peer created would be read back and never peer",
			"changeBgpPeersForNetwork",
			"changeBgpPeersForVpc",
			"createASNRange",
			"createBgpPeer",
			"createGuestNetworkIpv6Prefix",
			"createIpv4SubnetForGuestNetwork",
			"createIpv4SubnetForZone",
			"dedicateBgpPeer",
			"dedicateIpv4SubnetForZone",
			"deleteASNRange",
			"deleteBgpPeer",
			"deleteGuestNetworkIpv6Prefix",
			"deleteIpv4SubnetForGuestNetwork",
			"deleteIpv4SubnetForZone",
			"listASNRanges",
			"listASNumbers",
			"listBgpPeers",
			"listGuestNetworkIpv6Prefixes",
			"listIpv4SubnetsForGuestNetwork",
			"listIpv4SubnetsForZone",
			"releaseASNumber",
			"releaseBgpPeer",
			"releaseIpv4SubnetForZone",
			"updateBgpPeer",
			"updateIpv4SubnetForZone"),

		// Global load balancing and portable IP ranges.
		//
		// Both span zones, and the emulator has one: there is nothing to balance between and no second zone to move an
		// address to.
		because("global load balancing and portable IP ranges span zones and the emulator has one, so there is nothing to balance between and no second zone to move an address to",
			"assignToGlobalLoadBalancerRule",
			"createGlobalLoadBalancerRule",
			"createPortableIpRange",
			"deleteGlobalLoadBalancerRule",
			"deletePortableIpRange",
			"listGlobalLoadBalancerRules",
			"listPortableIpRanges",
			"removeFromGlobalLoadBalancerRule",
			"updateGlobalLoadBalancerRule"),

		// Image bytes: registering, uploading, extracting, copying.
		//
		// Each of these moves image bytes, which the emulator neither holds nor fetches from a URL. A template registered
		// here would be reported ready for something nobody downloaded, and a URL accepted would be one the emulator is
		// asked to fetch. The catalogue is what the operator declares (--strict-catalog), not something a client edits, so
		// updating and deleting a template or an ISO go with registering one. Offerings are NOT here: creating a service
		// offering is a record, and can be answered honestly, so it is backlog.
		because("registering, uploading, extracting or copying a template, ISO or volume moves image bytes the emulator neither holds nor fetches, so a template registered here would be reported ready for something nobody downloaded",
			"copyIso",
			"copySnapshot",
			"copyTemplate",
			"createTemplate",
			"deleteIso",
			"deleteTemplate",
			"downloadValidationScreenshot",
			"extractIso",
			"extractSnapshot",
			"extractTemplate",
			"extractVolume",
			"getUploadParamsForIso",
			"getUploadParamsForTemplate",
			"getUploadParamsForVolume",
			"prepareTemplate",
			"registerIso",
			"registerTemplate",
			"updateIso",
			"updateTemplate",
			"uploadVolume"),

		// Network function appliances.
		//
		// A VNF appliance is a virtual machine running a vendor's network function, deployed from a template of its own
		// kind. The emulator runs no such appliance and registers no template.
		because("a VNF appliance is a virtual machine running a vendor's network function from a template of its own kind, and the emulator runs no such appliance and registers no template",
			"deleteVnfTemplate",
			"deployVnfAppliance",
			"listVnfAppliances",
			"listVnfTemplates",
			"registerVnfTemplate",
			"updateVnfTemplate"),

		// Backup and recovery.
		//
		// A backup copies a machine's disks to a repository through a provider (NAS, Veeam, a dummy). The emulator holds
		// no disk content and runs no provider, so a backup taken here would be a record of nothing and a restore would
		// resurrect a machine from it. Snapshots are NOT here: they are metadata the other packs serve, and are backlog.
		because("a backup copies a machine's disks to a repository through a provider, and the emulator holds no disk content and runs no provider, so one taken here would be a record of nothing and a restore a machine out of it",
			"addBackupRepository",
			"assignVirtualMachineToBackupOffering",
			"cloneBackupOffering",
			"createBackup",
			"createBackupOffering",
			"createBackupSchedule",
			"createVMFromBackup",
			"deleteBackup",
			"deleteBackupOffering",
			"deleteBackupRepository",
			"deleteBackupSchedule",
			"finishBackupChain",
			"importBackupOffering",
			"listBackupOfferings",
			"listBackupProviderOfferings",
			"listBackupProviders",
			"listBackupRepositories",
			"listBackupSchedule",
			"listBackupServiceJobs",
			"listBackups",
			"removeVirtualMachineFromBackupOffering",
			"restoreBackup",
			"restoreVolumeFromBackupAndAttachToVM",
			"updateBackupOffering",
			"updateBackupRepository",
			"updateBackupSchedule"),

		// CloudStack Kubernetes Service.
		//
		// CKS builds a cluster out of virtual machines and an ISO of Kubernetes binaries, and keeps its own supported-
		// version catalogue. Outscale's OKS is declined for the same reason: answering any of it would describe a control
		// plane that does not run. The CNI configurations are CKS's, so they go with it.
		because("a managed Kubernetes service built from virtual machines and an ISO of binaries, with a control plane of its own, which no route here mounts: answering any of it would describe a cluster that does not run",
			"addKubernetesSupportedVersion",
			"addNodesToKubernetesCluster",
			"addVirtualMachinesToKubernetesCluster",
			"createKubernetesCluster",
			"deleteCniConfiguration",
			"deleteKubernetesCluster",
			"deleteKubernetesSupportedVersion",
			"getKubernetesClusterConfig",
			"getUploadParamsForKubernetesSupportedVersion",
			"listCniConfiguration",
			"listKubernetesClusters",
			"listKubernetesSupportedVersions",
			"registerCniConfiguration",
			"removeNodesFromKubernetesCluster",
			"removeVirtualMachinesFromKubernetesCluster",
			"scaleKubernetesCluster",
			"startKubernetesCluster",
			"stopKubernetesCluster",
			"updateKubernetesClusterAffinityGroups",
			"updateKubernetesSupportedVersion",
			"upgradeKubernetesCluster"),

		// Autoscaling.
		//
		// Autoscaling adds and removes machines from measured load, and no load is measured here. A policy served would be
		// read back and never applied, which is the access-control argument again: it describes a rule that nothing
		// applies.
		because("autoscaling adds and removes machines from measured load and no load is measured here, so a policy, a profile or a group served would describe a rule that nothing ever applies",
			"createAutoScalePolicy",
			"createAutoScaleVmGroup",
			"createAutoScaleVmProfile",
			"createCondition",
			"createCounter",
			"deleteAutoScalePolicy",
			"deleteAutoScaleVmGroup",
			"deleteAutoScaleVmProfile",
			"deleteCondition",
			"deleteCounter",
			"disableAutoScaleVmGroup",
			"enableAutoScaleVmGroup",
			"listAutoScalePolicies",
			"listAutoScaleVmGroups",
			"listAutoScaleVmProfiles",
			"listConditions",
			"listCounters",
			"updateAutoScalePolicy",
			"updateAutoScaleVmGroup",
			"updateAutoScaleVmProfile",
			"updateCondition"),

		// Schedules.
		//
		// A schedule acts on a resource at a time, and nothing here runs one: a schedule would be read back and never
		// fire.
		because("a schedule starts, stops or acts on a resource at a given time and nothing here runs one, so a schedule would be read back and never fire",
			"createResourceSchedule",
			"createVMSchedule",
			"deleteResourceSchedule",
			"deleteVMSchedule",
			"listResourceSchedule",
			"listVMSchedule",
			"updateResourceSchedule",
			"updateVMSchedule"),

		// Shared file systems.
		//
		// A shared file system is an appliance machine exporting a volume over NFS, built from a service offering and a
		// disk. The emulator can run none of it.
		because("a shared file system is an appliance machine exporting a volume over NFS, built from a service offering and a disk, none of which the emulator can run",
			"changeSharedFileSystemDiskOffering",
			"changeSharedFileSystemServiceOffering",
			"createSharedFileSystem",
			"destroySharedFileSystem",
			"expungeSharedFileSystem",
			"listSharedFileSystemProviders",
			"listSharedFileSystems",
			"recoverSharedFileSystem",
			"restartSharedFileSystem",
			"startSharedFileSystem",
			"stopSharedFileSystem",
			"updateSharedFileSystem"),

		// The DNS framework.
		//
		// It publishes records through an external provider (PowerDNS is the first), and the emulator has none, so a
		// record served here would never resolve.
		because("the DNS framework publishes records through an external provider and the emulator has none, so a zone or a record served here would never resolve",
			"addDnsServer",
			"associateDnsZoneToNetwork",
			"createDnsRecord",
			"createDnsZone",
			"deleteDnsRecord",
			"deleteDnsServer",
			"deleteDnsZone",
			"disassociateDnsZoneFromNetwork",
			"listDnsProviders",
			"listDnsRecords",
			"listDnsServers",
			"listDnsZones",
			"updateDnsServer",
			"updateDnsZone"),

		// Themes and icons of the web interface.
		//
		// The emulator does not serve CloudStack's web interface.
		because("themes and icons customise CloudStack's web interface, which the emulator does not serve",
			"createGuiTheme",
			"deleteResourceIcon",
			"listGuiThemes",
			"listResourceIcon",
			"removeGuiTheme",
			"updateGuiTheme",
			"uploadResourceIcon"),

		// Webhooks.
		//
		// A webhook makes the server send HTTP requests to a URL the client names. The emulator makes none, which is also
		// what keeps it from being pointed at a network it can reach.
		because("a webhook makes the server send HTTP requests to a URL the client names, and the emulator makes none, which also keeps it from being pointed at a network it can reach",
			"addWebhookFilter",
			"createWebhook",
			"deleteWebhook",
			"deleteWebhookDelivery",
			"deleteWebhookFilter",
			"executeWebhookDelivery",
			"listWebhookDeliveries",
			"listWebhookFilters",
			"listWebhooks",
			"updateWebhook"),

		// Extensions and custom actions.
		//
		// These run scripts on the management server or its hosts. The emulator executes nothing a client uploads.
		because("extensions and custom actions run scripts on the management server or its hosts, and the emulator executes nothing a client uploads",
			"addCustomAction",
			"createExtension",
			"deleteCustomAction",
			"deleteExtension",
			"listCustomActions",
			"listExtensions",
			"registerExtension",
			"runCustomAction",
			"unregisterExtension",
			"updateCustomAction",
			"updateExtension",
			"updateRegisteredExtension"),

		// Key management and HSM profiles.
		//
		// These hold key material and encrypt volumes with it. The emulator holds no key and encrypts nothing, so a key
		// rotated here would protect nothing and read as if it did.
		because("key management and HSM profiles hold key material and encrypt volumes with it, and the emulator holds no key and encrypts nothing, so a key rotated here would protect nothing",
			"createHSMProfile",
			"createKMSKey",
			"deleteHSMProfile",
			"deleteKMSKey",
			"listHSMProfiles",
			"listKMSKeys",
			"migrateVolumesToKMS",
			"rotateKMSKey",
			"updateHSMProfile",
			"updateKMSKey"),

		// Vendor appliances, bare metal and external storage integrations.
		//
		// Each of these configures a device or a service the emulator does not run: a Netscaler, a Palo Alto firewall, a
		// Nicira or BigSwitch or OpenDaylight controller, a Cisco UCS manager, a bare-metal PXE and DHCP service, a
		// SolidFire or Elastistor storage system, an OVS element, a traffic monitor, a generic network device. Answering
		// would configure a device that does not exist, and a client that then read back its health would be told what
		// nobody measured.
		because("a plugin for a vendor appliance or a provisioning service (Netscaler, Palo Alto, Nicira, UCS, SolidFire, PXE and DHCP for bare metal) that the emulator does not run, so answering would configure a device that does not exist",
			"addBaremetalDhcp",
			"addBaremetalHost",
			"addBaremetalPxeKickStartServer",
			"addBaremetalPxePingServer",
			"addBaremetalRct",
			"addBigSwitchBcfDevice",
			"addBrocadeVcsDevice",
			"addGloboDnsHost",
			"addNetscalerLoadBalancer",
			"addNetworkDevice",
			"addNiciraNvpDevice",
			"addOpenDaylightController",
			"addPaloAltoFirewall",
			"addStratosphereSsp",
			"addTrafficMonitor",
			"addUcsManager",
			"associateUcsProfileToBlade",
			"configureNetscalerLoadBalancer",
			"configureOvsElement",
			"configurePaloAltoFirewall",
			"deleteBaremetalRct",
			"deleteBigSwitchBcfDevice",
			"deleteBrocadeVcsDevice",
			"deleteNetscalerControlCenter",
			"deleteNetscalerLoadBalancer",
			"deleteNetworkDevice",
			"deleteNiciraNvpDevice",
			"deleteOpenDaylightController",
			"deletePaloAltoFirewall",
			"deleteServicePackageOffering",
			"deleteStratosphereSsp",
			"deleteTrafficMonitor",
			"deleteUcsManager",
			"deployNetscalerVpx",
			"getSolidFireAccountId",
			"getSolidFireVolumeAccessGroupIds",
			"getSolidFireVolumeSize",
			"listBaremetalDhcp",
			"listBaremetalPxeServers",
			"listBaremetalRct",
			"listBigSwitchBcfDevices",
			"listBrocadeVcsDeviceNetworks",
			"listBrocadeVcsDevices",
			"listElastistorInterface",
			"listElastistorPool",
			"listElastistorVolume",
			"listNetscalerControlCenter",
			"listNetscalerLoadBalancerNetworks",
			"listNetscalerLoadBalancers",
			"listNetworkDevice",
			"listNiciraNvpDeviceNetworks",
			"listNiciraNvpDevices",
			"listOpenDaylightControllers",
			"listOvsElements",
			"listPaloAltoFirewallNetworks",
			"listPaloAltoFirewalls",
			"listRegisteredServicePackages",
			"listTrafficMonitors",
			"listUcsBlades",
			"listUcsManagers",
			"listUcsProfiles",
			"notifyBaremetalProvisionDone",
			"registerNetscalerControlCenter",
			"registerNetscalerServicePackage",
			"stopNetScalerVpx"),
	)
}
