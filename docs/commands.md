# Справка по командам

<!-- Файл сгенерирован из дерева команд (cobra/doc). Руками не править:
     go test ./internal/cli -run TestCommandsDoc -update-docs
     CI сверяет файл с бинарём (TestCommandsDoc). -->

Справка — на английском, как `--help` (решение D6); то же самое печатает
`evo <команда> --help`. Описание по-русски — в [README](../README.md)
и соседних файлах `docs/`.

## Содержание

- [evo](#evo)
  - [evo completion](#evo-completion)
    - [evo completion bash](#evo-completion-bash)
    - [evo completion fish](#evo-completion-fish)
    - [evo completion powershell](#evo-completion-powershell)
    - [evo completion zsh](#evo-completion-zsh)
  - [evo configure](#evo-configure)
    - [evo configure delete](#evo-configure-delete)
    - [evo configure list](#evo-configure-list)
  - [evo deploy](#evo-deploy)
  - [evo destroy](#evo-destroy)
  - [evo disk](#evo-disk)
    - [evo disk attach](#evo-disk-attach)
    - [evo disk create](#evo-disk-create)
    - [evo disk delete](#evo-disk-delete)
    - [evo disk detach](#evo-disk-detach)
    - [evo disk list](#evo-disk-list)
    - [evo disk resize](#evo-disk-resize)
    - [evo disk types](#evo-disk-types)
  - [evo doctor](#evo-doctor)
  - [evo init](#evo-init)
  - [evo k8s](#evo-k8s)
    - [evo k8s add-nodepool](#evo-k8s-add-nodepool)
    - [evo k8s apply](#evo-k8s-apply)
    - [evo k8s create](#evo-k8s-create)
    - [evo k8s delete](#evo-k8s-delete)
    - [evo k8s get](#evo-k8s-get)
    - [evo k8s helm](#evo-k8s-helm)
    - [evo k8s kubeconfig](#evo-k8s-kubeconfig)
    - [evo k8s kubectl](#evo-k8s-kubectl)
    - [evo k8s list](#evo-k8s-list)
    - [evo k8s nodepools](#evo-k8s-nodepools)
    - [evo k8s options](#evo-k8s-options)
    - [evo k8s plan](#evo-k8s-plan)
  - [evo list-images](#evo-list-images)
  - [evo list-zones](#evo-list-zones)
  - [evo logs](#evo-logs)
  - [evo mcp](#evo-mcp)
  - [evo pg](#evo-pg)
    - [evo pg backup-create](#evo-pg-backup-create)
    - [evo pg backups](#evo-pg-backups)
    - [evo pg create](#evo-pg-create)
    - [evo pg databases](#evo-pg-databases)
    - [evo pg db-create](#evo-pg-db-create)
    - [evo pg db-delete](#evo-pg-db-delete)
    - [evo pg delete](#evo-pg-delete)
    - [evo pg get](#evo-pg-get)
    - [evo pg list](#evo-pg-list)
    - [evo pg operation](#evo-pg-operation)
    - [evo pg specs](#evo-pg-specs)
    - [evo pg user-create](#evo-pg-user-create)
    - [evo pg user-delete](#evo-pg-user-delete)
    - [evo pg users](#evo-pg-users)
    - [evo pg versions](#evo-pg-versions)
  - [evo s3](#evo-s3)
    - [evo s3 bucket-create](#evo-s3-bucket-create)
    - [evo s3 bucket-delete](#evo-s3-bucket-delete)
    - [evo s3 buckets](#evo-s3-buckets)
    - [evo s3 get](#evo-s3-get)
    - [evo s3 ls](#evo-s3-ls)
    - [evo s3 put](#evo-s3-put)
    - [evo s3 rm](#evo-s3-rm)
  - [evo secret](#evo-secret)
    - [evo secret add-version](#evo-secret-add-version)
    - [evo secret create](#evo-secret-create)
    - [evo secret delete](#evo-secret-delete)
    - [evo secret get](#evo-secret-get)
    - [evo secret list](#evo-secret-list)
    - [evo secret value](#evo-secret-value)
  - [evo status](#evo-status)
  - [evo tools](#evo-tools)
    - [evo tools call](#evo-tools-call)
    - [evo tools describe](#evo-tools-describe)
    - [evo tools list](#evo-tools-list)
  - [evo verify](#evo-verify)
  - [evo version](#evo-version)
  - [evo vm](#evo-vm)
    - [evo vm create](#evo-vm-create)
    - [evo vm delete](#evo-vm-delete)
    - [evo vm flavors](#evo-vm-flavors)
    - [evo vm get](#evo-vm-get)
    - [evo vm list](#evo-vm-list)
    - [evo vm zones](#evo-vm-zones)
  - [evo vpc](#evo-vpc)
    - [evo vpc create](#evo-vpc-create)
    - [evo vpc delete](#evo-vpc-delete)
    - [evo vpc fip-attach](#evo-vpc-fip-attach)
    - [evo vpc fip-release](#evo-vpc-fip-release)
    - [evo vpc fips](#evo-vpc-fips)
    - [evo vpc get](#evo-vpc-get)
    - [evo vpc list](#evo-vpc-list)
    - [evo vpc operation](#evo-vpc-operation)
    - [evo vpc route-create](#evo-vpc-route-create)
    - [evo vpc route-delete](#evo-vpc-route-delete)
    - [evo vpc routes](#evo-vpc-routes)
    - [evo vpc security-groups](#evo-vpc-security-groups)
    - [evo vpc subnets](#evo-vpc-subnets)
  - [evo whoami](#evo-whoami)

## evo

Deploy docker-compose projects to Cloud.ru VMs

### Synopsis

```text
Deploy docker-compose projects to Cloud.ru VMs.

Exit codes:
  0    success
  1    general error (including unknown command or flag)
  2    authentication or configuration error
  3    confirmation required (not given, or stdin is not a terminal; use --yes)
  4    object not found
  5    Cloud.ru API error (HTTP 4xx/5xx other than authentication)
  130  interrupted by the user
```

### Options

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
  -h, --help                help for evo
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo completion](#evo-completion)	 - Generate the autocompletion script for the specified shell
* [evo configure](#evo-configure)	 - Set up credentials and the default project for a profile
* [evo deploy](#evo-deploy)	 - Deploy a docker-compose project to a Cloud.ru VM
* [evo destroy](#evo-destroy)	 - Delete the deployed VM and clean up resources
* [evo disk](#evo-disk)	 - Disks
* [evo doctor](#evo-doctor)	 - Check configuration, credentials and connectivity
* [evo init](#evo-init)	 - Create .evo.yaml with a zone and image chosen from the Cloud.ru API
* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters
* [evo list-images](#evo-list-images)	 - List available VM boot images
* [evo list-zones](#evo-list-zones)	 - List available availability zones
* [evo logs](#evo-logs)	 - View container logs from the deployed VM
* [evo mcp](#evo-mcp)	 - Run as an MCP server (stdio, or Streamable HTTP with --http) for AI agents
* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups
* [evo s3](#evo-s3)	 - Object Storage: buckets and objects
* [evo secret](#evo-secret)	 - Secret Manager
* [evo status](#evo-status)	 - Show current deployment status
* [evo tools](#evo-tools)	 - List, describe and call any of the evo tools (the same ones the MCP server exposes)
* [evo verify](#evo-verify)	 - Run health checks against the deployed application (exit 1 if unhealthy)
* [evo version](#evo-version)	 - Print the version, commit, build date, Go version and platform
* [evo vm](#evo-vm)	 - Virtual machines
* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs
* [evo whoami](#evo-whoami)	 - Show the active profile and check its credentials

## evo completion

Generate the autocompletion script for the specified shell

### Synopsis

```text
Generate the autocompletion script for evo for the specified shell.
See each sub-command's help for details on how to use the generated script.
```

### Options

```
  -h, --help   help for completion
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs
* [evo completion bash](#evo-completion-bash)	 - Generate the autocompletion script for bash
* [evo completion fish](#evo-completion-fish)	 - Generate the autocompletion script for fish
* [evo completion powershell](#evo-completion-powershell)	 - Generate the autocompletion script for powershell
* [evo completion zsh](#evo-completion-zsh)	 - Generate the autocompletion script for zsh

## evo completion bash

Generate the autocompletion script for bash

### Synopsis

```text
Generate the autocompletion script for the bash shell.

This script depends on the 'bash-completion' package.
If it is not installed already, you can install it via your OS's package manager.

To load completions in your current shell session:

	source <(evo completion bash)

To load completions for every new session, execute once:

#### Linux:

	evo completion bash > /etc/bash_completion.d/evo

#### macOS:

	evo completion bash > $(brew --prefix)/etc/bash_completion.d/evo

You will need to start a new shell for this setup to take effect.
```

```
evo completion bash
```

### Options

```
  -h, --help              help for bash
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo completion](#evo-completion)	 - Generate the autocompletion script for the specified shell

## evo completion fish

Generate the autocompletion script for fish

### Synopsis

```text
Generate the autocompletion script for the fish shell.

To load completions in your current shell session:

	evo completion fish | source

To load completions for every new session, execute once:

	evo completion fish > ~/.config/fish/completions/evo.fish

You will need to start a new shell for this setup to take effect.
```

```
evo completion fish [flags]
```

### Options

```
  -h, --help              help for fish
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo completion](#evo-completion)	 - Generate the autocompletion script for the specified shell

## evo completion powershell

Generate the autocompletion script for powershell

### Synopsis

```text
Generate the autocompletion script for powershell.

To load completions in your current shell session:

	evo completion powershell | Out-String | Invoke-Expression

To load completions for every new session, add the output of the above command
to your powershell profile.
```

```
evo completion powershell [flags]
```

### Options

```
  -h, --help              help for powershell
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo completion](#evo-completion)	 - Generate the autocompletion script for the specified shell

## evo completion zsh

Generate the autocompletion script for zsh

### Synopsis

```text
Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need
to enable it.  You can execute the following once:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

To load completions in your current shell session:

	source <(evo completion zsh)

To load completions for every new session, execute once:

#### Linux:

	evo completion zsh > "${fpath[1]}/_evo"

#### macOS:

	evo completion zsh > $(brew --prefix)/share/zsh/site-functions/_evo

You will need to start a new shell for this setup to take effect.
```

```
evo completion zsh [flags]
```

### Options

```
  -h, --help              help for zsh
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo completion](#evo-completion)	 - Generate the autocompletion script for the specified shell

## evo configure

Set up credentials and the default project for a profile

### Synopsis

```text
Store a service account access key and a default project in a profile.

Interactively asks for the key ID and the secret (not echoed), checks them
against Cloud.ru IAM, then lists the projects the key can see and lets you
pick one (a single project is picked automatically).

Non-interactive (CI, scripts) — the secret is read from stdin, never from a flag:
  printf '%s' "$SECRET" | evo configure --key-id KEY_ID --secret-stdin --project-id PROJECT_ID

Files (mode 0600, directory 0700; CLOUDRU_CONFIG_DIR overrides the directory):
  <user config dir>/evo/config.yaml        project_id, region, defaults
  <user config dir>/evo/credentials.yaml   key_id, secret, optional s3 section

Required access: the service account needs a project role that allows managing
Compute and VPC resources; listing projects also needs read access to the
organization (otherwise pass --project-id). Details:
https://github.com/shatim-ops/evo-cli/blob/main/docs/auth.md
```

```
evo configure [flags]
```

### Options

```
  -h, --help                help for configure
      --key-id string       Access key ID of the service account
      --project-id string   Cloud.ru project UUID (skips the project list)
      --region string       Default availability zone UUID for the profile
      --secret-stdin        Read the secret from the first line of stdin (non-interactive)
```

### Options inherited from parent commands

```
      --dry-run          Show what would be created or deleted (with a cost estimate) and exit
      --json             Output in JSON format
      --no-color         Disable colored output
      --profile string   Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --verbose          Enable verbose logging
  -y, --yes              Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs
* [evo configure delete](#evo-configure-delete)	 - Delete a stored profile and its cached token
* [evo configure list](#evo-configure-list)	 - List stored profiles

## evo configure delete

Delete a stored profile and its cached token

```
evo configure delete <profile> [flags]
```

### Options

```
  -h, --help   help for delete
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo configure](#evo-configure)	 - Set up credentials and the default project for a profile

## evo configure list

List stored profiles

```
evo configure list [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo configure](#evo-configure)	 - Set up credentials and the default project for a profile

## evo deploy

Deploy a docker-compose project to a Cloud.ru VM

```
evo deploy [flags]
```

### Options

```
  -f, --compose-file string   Path to docker-compose file (default: docker-compose.yml)
      --disk-size int         Boot disk size in GB (default: 10)
      --flavor string         VM flavor (default: auto)
      --force                 Force redeploy even if compose hash unchanged
  -h, --help                  help for deploy
      --image-id string       Boot image UUID
      --non-interactive       Never prompt; creating a VM then requires --yes
      --project-id string     Cloud.ru project UUID
      --region string         Availability zone UUID
```

### Options inherited from parent commands

```
      --dry-run          Show what would be created or deleted (with a cost estimate) and exit
      --json             Output in JSON format
      --no-color         Disable colored output
      --profile string   Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --verbose          Enable verbose logging
  -y, --yes              Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs

## evo destroy

Delete the deployed VM and clean up resources

```
evo destroy [flags]
```

### Options

```
      --force   Skip confirmation prompt (same as --yes)
  -h, --help    help for destroy
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs

## evo disk

Disks

```
evo disk [flags]
```

### Options

```
  -h, --help   help for disk
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs
* [evo disk attach](#evo-disk-attach)	 - Attach an existing disk to a VM (POST /disks/{id}/attach).
* [evo disk create](#evo-disk-create)	 - Create a block-storage disk.
* [evo disk delete](#evo-disk-delete)	 - Permanently delete a disk and all its data (irreversible).
* [evo disk detach](#evo-disk-detach)	 - Detach a disk from a VM (POST /disks/{id}/detach).
* [evo disk list](#evo-disk-list)	 - List block-storage disks (id, name, size_gb, state, disk_type, zone, attached VMs).
* [evo disk resize](#evo-disk-resize)	 - Grow a disk in place via PUT /disks/{id} (up to 65536 GB).
* [evo disk types](#evo-disk-types)	 - List available Cloud.ru disk types (UUID + name, e.g. SSD/HDD).

## evo disk attach

Attach an existing disk to a VM (POST /disks/{id}/attach).

### Synopsis

```text
Attach an existing disk to a VM (POST /disks/{id}/attach). Disk and VM must be in the same zone. Mutation: requires confirmed=true.

Runs the disk_attach tool (see 'evo tools describe disk_attach').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo disk attach <disk-id> [flags]
```

### Options

```
  -h, --help        help for attach
      --vm string   VM name or UUID.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Disks

## evo disk create

Create a block-storage disk.

### Synopsis

```text
Create a block-storage disk. Pass attach_to=<VM name/uuid> to create it in that VM's zone and (with confirmed) attach it in one step — disks must be in the SAME zone as the VM. NB: attaching adds a NEW block device; it does NOT grow the VM's existing filesystem (you then partition/format/mount it on the VM). Resizing the boot disk in place is NOT available via this API (console-only). Mutation (billable): requires confirmed=true.

Runs the disk_create tool (see 'evo tools describe disk_create').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo disk create <name> [flags]
```

### Options

```
      --attach-to string   VM name or UUID to create-in-zone and auto-attach to (e.g. 'vm-712c44').
      --disk-type string   Disk type name (e.g. from list_disk_types) or UUID. Default: an SSD type.
  -h, --help               help for create
      --size-gb int        Disk size in GB (required).
      --zone string        Availability zone name or UUID (alternative to attach_to; default: configured region).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Disks

## evo disk delete

Permanently delete a disk and all its data (irreversible).

### Synopsis

```text
Permanently delete a disk and all its data (irreversible). Detach it first if attached. Mutation: requires confirmed=true.

Runs the disk_delete tool (see 'evo tools describe disk_delete').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo disk delete <disk-id> [flags]
```

### Options

```
  -h, --help   help for delete
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Disks

## evo disk detach

Detach a disk from a VM (POST /disks/{id}/detach).

### Synopsis

```text
Detach a disk from a VM (POST /disks/{id}/detach). Do NOT detach a bootable/in-use disk carelessly. Mutation: requires confirmed=true.

Runs the disk_detach tool (see 'evo tools describe disk_detach').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo disk detach <disk-id> [flags]
```

### Options

```
  -h, --help        help for detach
      --vm string   VM name or UUID the disk is attached to.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Disks

## evo disk list

List block-storage disks (id, name, size_gb, state, disk_type, zone, attached VMs).

### Synopsis

```text
List block-storage disks (id, name, size_gb, state, disk_type, zone, attached VMs). Read-only.

Runs the disk_list tool (see 'evo tools describe disk_list').
```

```
evo disk list [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Disks

## evo disk resize

Grow a disk in place via PUT /disks/{id} (up to 65536 GB).

### Synopsis

```text
Grow a disk in place via PUT /disks/{id} (up to 65536 GB). GROW ONLY — the API rejects shrink and FREE-TIER disks (free_tier_disk_resize_is_prohibited). After resizing, grow the partition+filesystem on the VM (growpart + resize2fs). This is how you'd enlarge a VM's disk without a new volume — but a free-tier boot disk can't be resized (recreate the VM with a paid disk instead). Mutation: requires confirmed=true.

Runs the disk_resize tool (see 'evo tools describe disk_resize').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo disk resize <disk-id> [flags]
```

### Options

```
  -h, --help          help for resize
      --size-gb int   New size in GB (must be larger than current; max 65536).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Disks

## evo disk types

List available Cloud.ru disk types (UUID + name, e.g. SSD/HDD).

### Synopsis

```text
List available Cloud.ru disk types (UUID + name, e.g. SSD/HDD). Use a name as the 'disk_type' argument for deploy_plan in manual mode.

Runs the list_disk_types tool (see 'evo tools describe list_disk_types').
```

```
evo disk types [flags]
```

### Options

```
  -h, --help   help for types
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Disks

## evo doctor

Check configuration, credentials and connectivity

### Synopsis

```text
Check the setup: config files and their permissions, where each setting
comes from, reachability of the IAM and Compute APIs, the credentials, and
kubectl/helm in PATH (needed only by the k8s tools; a warning, not an error).

Exits with 2 if the configuration or credentials are broken, 1 if only
connectivity checks failed.
```

```
evo doctor [flags]
```

### Options

```
  -h, --help   help for doctor
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs

## evo init

Create .evo.yaml with a zone and image chosen from the Cloud.ru API

### Synopsis

```text
Create .evo.yaml for this project.

The availability zone and the boot image are listed from the Cloud.ru API
(credentials required). On a terminal you choose from a numbered list;
with --non-interactive or without a terminal the first zone and the newest
plain Ubuntu (else Debian) image are taken, and the choice is printed.
```

```
evo init [flags]
```

### Options

```
  -h, --help              help for init
      --non-interactive   Take the first zone and the preferred image without asking
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs

## evo k8s

Managed Kubernetes clusters

```
evo k8s [flags]
```

### Options

```
  -h, --help   help for k8s
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs
* [evo k8s add-nodepool](#evo-k8s-add-nodepool)	 - Add a worker node pool to a Managed Kubernetes cluster (workers to run pods — a fresh cluster has n…
* [evo k8s apply](#evo-k8s-apply)	 - Apply a Kubernetes manifest (YAML/JSON, may be multi-document) to a cluster — like `kubectl apply -…
* [evo k8s create](#evo-k8s-create)	 - STEP 2 for Managed Kubernetes: create the cluster resolved by k8s_plan.
* [evo k8s delete](#evo-k8s-delete)	 - Permanently delete a Managed Kubernetes cluster.
* [evo k8s get](#evo-k8s-get)	 - Get a Managed Kubernetes cluster by id (full details incl.
* [evo k8s helm](#evo-k8s-helm)	 - Drive Helm on a cluster: action=install|upgrade|uninstall|list|status.
* [evo k8s kubeconfig](#evo-k8s-kubeconfig)	 - Get the kubeconfig for a Managed Kubernetes cluster.
* [evo k8s kubectl](#evo-k8s-kubectl)	 - Run ANY kubectl command against a cluster (auto-embedded token — no cloudlogin needed).
* [evo k8s list](#evo-k8s-list)	 - List Managed Kubernetes (mk8s) clusters in the project (id, name, status).
* [evo k8s nodepools](#evo-k8s-nodepools)	 - List node pools of a Managed Kubernetes cluster (name, state, desired/ready nodes).
* [evo k8s options](#evo-k8s-options)	 - List available Kubernetes versions (by release channel), node flavors and zones for mk8s.
* [evo k8s plan](#evo-k8s-plan)	 - STEP 1 for Managed Kubernetes: resolve a cluster config (k8s version, control-plane flavor, master …

## evo k8s add-nodepool

Add a worker node pool to a Managed Kubernetes cluster (workers to run pods — a fresh cluster has n…

### Synopsis

```text
Add a worker node pool to a Managed Kubernetes cluster (workers to run pods — a fresh cluster has none). REQUIRES confirmed=true after the user approves (billable). Async. Auto-resolves flavor (cpu/ram or name), nodes subnet and security group. NB: Cloud.ru's node-pool backend occasionally returns 503 'no healthy upstream' (transient outage) — just retry.

Runs the k8s_add_nodepool tool (see 'evo tools describe k8s_add_nodepool').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo k8s add-nodepool <cluster-id> [flags]
```

### Options

```
      --count int                  Number of worker nodes (fixed scale, default 2).
      --cpu int                    Node vCPUs for auto flavor (default 2).
      --disk-gb int                Node disk GB, min 10 (default 20).
      --flavor string              Node flavor name (see k8s_options); overrides cpu/ram_gb.
  -h, --help                       help for add-nodepool
      --name string                Node pool name, 4-60 (default nodepool-1).
      --nodes-subnet-id string     Subnet UUID for worker nodes (default auto zone-matched).
      --ram-gb int                 Node RAM GB for auto flavor (default 4).
      --security-group-id string   Security group UUID for nodes (default auto zone-matched SSH-access).
      --zone string                Zone name (default from productConfiguration).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters

## evo k8s apply

Apply a Kubernetes manifest (YAML/JSON, may be multi-document) to a cluster — like `kubectl apply -…

### Synopsis

```text
Apply a Kubernetes manifest (YAML/JSON, may be multi-document) to a cluster — like `kubectl apply -f -`. For deploying/updating raw workloads (Deployments, StatefulSets, Services, ServiceMonitors, etc.). Mutation: requires confirmed=true after the user approves. NB: a fresh cluster needs egress (snat_create) to pull public images, and CRDs (e.g. ServiceMonitor) must exist before objects that use them.

Runs the k8s_apply tool (see 'evo tools describe k8s_apply').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo k8s apply <cluster-id> [flags]
```

### Options

```
  -h, --help               help for apply
      --manifest string    The manifest YAML/JSON to apply (multi-doc allowed).
      --namespace string   Namespace to apply into (optional; manifests may set their own).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters

## evo k8s create

STEP 2 for Managed Kubernetes: create the cluster resolved by k8s_plan.

### Synopsis

```text
STEP 2 for Managed Kubernetes: create the cluster resolved by k8s_plan. REQUIRES confirmed=true after the user approves (billable). Async — returns cluster_id + operation_id; the cluster provisions in ~10-20 min (poll k8s_get). Accepts the same args as k8s_plan.

Runs the k8s_create tool (see 'evo tools describe k8s_create').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo k8s create [flags]
```

### Options

```
      --cluster-sa-id string   Cluster service-account UUID.
      --cpu int                Control-plane vCPUs.
      --flavor string          Control-plane flavor name.
  -h, --help                   help for create
      --kube-api-internet      Publish kube-apiserver to internet.
      --master-count int       1, 3 or 5.
      --name string            Cluster name.
      --product string         What the cluster is for.
      --ram-gb int             Control-plane RAM GB.
      --subnet-id string       Control-plane VIP subnet UUID.
      --version string         Kubernetes version.
      --zone string            Zone name.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters

## evo k8s delete

Permanently delete a Managed Kubernetes cluster.

### Synopsis

```text
Permanently delete a Managed Kubernetes cluster. REQUIRES confirmed=true after the user approves. Async.

Runs the k8s_delete tool (see 'evo tools describe k8s_delete').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo k8s delete <cluster-id> [flags]
```

### Options

```
  -h, --help   help for delete
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters

## evo k8s get

Get a Managed Kubernetes cluster by id (full details incl.

### Synopsis

```text
Get a Managed Kubernetes cluster by id (full details incl. status). Read-only.

Runs the k8s_get tool (see 'evo tools describe k8s_get').
```

```
evo k8s get <cluster-id> [flags]
```

### Options

```
  -h, --help   help for get
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters

## evo k8s helm

Drive Helm on a cluster: action=install|upgrade|uninstall|list|status.

### Synopsis

```text
Drive Helm on a cluster: action=install|upgrade|uninstall|list|status. install/upgrade are idempotent (helm upgrade --install --wait). Optionally add a chart repo via repo_url. Pass values (inline YAML) and/or set (array of key=value). Mutating actions require confirmed=true. Ideal for standard products (e.g. kube-prometheus-stack, databases). NB: nodes need egress (snat_create) to pull chart images; size the node pool for the chart (monitoring stacks want ≥8GB nodes).

Runs the k8s_helm tool (see 'evo tools describe k8s_helm').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo k8s helm <cluster-id> <action> [flags]
```

### Options

```
      --chart string       Chart ref, e.g. 'prometheus-community/kube-prometheus-stack' (install/upgrade).
  -h, --help               help for helm
      --namespace string   Target namespace (created on install/upgrade).
      --release string     Helm release name (install/upgrade/uninstall/status).
      --repo-url string    Chart repo URL to add+update first; repo name taken from the chart's '<repo>/' prefix.
      --set stringArray    Array of 'key=value' Helm --set overrides (optional). (repeatable, or comma-separated)
      --values string      Inline values.yaml content (optional).
      --version string     Chart version (optional; default latest).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters

## evo k8s kubeconfig

Get the kubeconfig for a Managed Kubernetes cluster.

### Synopsis

```text
Get the kubeconfig for a Managed Kubernetes cluster. By DEFAULT it embeds a static bearer token so kubectl/helm work as-is (the raw Cloud.ru kubeconfig needs a 'cloudlogin' exec-plugin that isn't installed → kubectl would fail 'executable cloudlogin not found'). Token TTL ~1h. Pass embed_token=false for the raw exec-plugin kubeconfig.

Runs the k8s_kubeconfig tool (see 'evo tools describe k8s_kubeconfig').
```

```
evo k8s kubeconfig <cluster-id> [flags]
```

### Options

```
      --embed-token      Embed a static bearer token so kubectl works without the cloudlogin plugin (default true).
  -h, --help             help for kubeconfig
      --network string   NETWORK_TYPE_PUBLIC or NETWORK_TYPE_PRIVATE (default: cluster default).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters

## evo k8s kubectl

Run ANY kubectl command against a cluster (auto-embedded token — no cloudlogin needed).

### Synopsis

```text
Run ANY kubectl command against a cluster (auto-embedded token — no cloudlogin needed). Read verbs (get/describe/logs/top/events/'rollout status'/explain/…) run freely for inspection & verification. Mutating verbs (create/delete/patch/scale/label/'rollout restart'/exec/cordon/…) require confirmed=true. General escape hatch for whatever a product needs beyond helm/apply. Example args: "get pods -A", "-n db logs deploy/postgres", "create secret generic s --from-literal=k=v".

Runs the k8s_kubectl tool (see 'evo tools describe k8s_kubectl').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo k8s kubectl <cluster-id> <args>... [flags]
```

### Options

```
  -h, --help   help for kubectl
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters

## evo k8s list

List Managed Kubernetes (mk8s) clusters in the project (id, name, status).

### Synopsis

```text
List Managed Kubernetes (mk8s) clusters in the project (id, name, status). Read-only.

Runs the k8s_list tool (see 'evo tools describe k8s_list').
```

```
evo k8s list [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters

## evo k8s nodepools

List node pools of a Managed Kubernetes cluster (name, state, desired/ready nodes).

### Synopsis

```text
List node pools of a Managed Kubernetes cluster (name, state, desired/ready nodes). Read-only.

Runs the k8s_nodepools tool (see 'evo tools describe k8s_nodepools').
```

```
evo k8s nodepools <cluster-id> [flags]
```

### Options

```
  -h, --help   help for nodepools
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters

## evo k8s options

List available Kubernetes versions (by release channel), node flavors and zones for mk8s.

### Synopsis

```text
List available Kubernetes versions (by release channel), node flavors and zones for mk8s. Read-only. Use before k8s_plan in manual mode.

Runs the k8s_options tool (see 'evo tools describe k8s_options').
```

```
evo k8s options [flags]
```

### Options

```
  -h, --help   help for options
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters

## evo k8s plan

STEP 1 for Managed Kubernetes: resolve a cluster config (k8s version, control-plane flavor, master …

### Synopsis

```text
STEP 1 for Managed Kubernetes: resolve a cluster config (k8s version, control-plane flavor, master count, subnet, service account) WITHOUT creating anything. Show it to the user, then call k8s_create with the same args + confirmed=true.

Runs the k8s_plan tool (see 'evo tools describe k8s_plan').
```

```
evo k8s plan [flags]
```

### Options

```
      --cluster-sa-id string   Cluster service-account UUID (default: the agent's own service account).
      --cpu int                Control-plane vCPUs for auto flavor pick (default 2).
      --flavor string          Control-plane flavor name (see k8s_options); overrides cpu/ram_gb.
  -h, --help                   help for plan
      --kube-api-internet      Publish kube-apiserver to the internet (default true).
      --master-count int       Control-plane node count: 1, 3 or 5 (default 1).
      --name string            Cluster name (3-60; default k8s-<product>).
      --product string         What the cluster is for (used for the name).
      --ram-gb int             Control-plane RAM GB for auto flavor pick (default 4).
      --subnet-id string       Subnet UUID for the control-plane private VIP (default: auto zone-matched).
      --version string         Kubernetes version, e.g. v1.34.6 (default: newest STABLE; see k8s_options).
      --zone string            Availability zone name, e.g. ru.AZ-2 (default from productConfiguration).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Managed Kubernetes clusters

## evo list-images

List available VM boot images

### Synopsis

```text
List boot images from the Cloud.ru API.

Use the image ID as the --image-id flag or CLOUDRU_IMAGE_ID env var for deploy.
deploy installs Docker on Ubuntu and Debian images (e.g. Ubuntu-24.04).
Use --json for machine-readable output.
```

```
evo list-images [flags]
```

### Options

```
  -h, --help   help for list-images
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs

## evo list-zones

List available availability zones

### Synopsis

```text
List availability zones from the Cloud.ru API.

Use the zone ID as the --region flag or CLOUDRU_REGION env var for deploy.
Use --json for machine-readable output.
```

```
evo list-zones [flags]
```

### Options

```
  -h, --help   help for list-zones
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs

## evo logs

View container logs from the deployed VM

```
evo logs [flags]
```

### Options

```
  -f, --follow           Follow log output
  -h, --help             help for logs
  -s, --service string   Filter logs by service name
  -n, --tail int         Number of lines to show (0 = all) (default 100)
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs

## evo mcp

Run as an MCP server (stdio, or Streamable HTTP with --http) for AI agents

### Synopsis

```text
Run evo as a Model Context Protocol server.

The server exposes the evo tools (VMs, disks, networks, Kubernetes,
databases, ...) so an AI agent (e.g. Claude Code) can manage Cloud.ru
resources directly.

By default it speaks newline-delimited JSON-RPC 2.0 on stdin/stdout; all
human-readable output is suppressed (stdout is reserved for the protocol).
SIGINT/SIGTERM and notifications/cancelled cancel running tool calls;
closing stdin does not (calls already sent finish and are answered). Add it to Claude Code with:
  claude mcp add --transport stdio --scope project evo -- evo mcp

With --http ADDR it serves the MCP Streamable HTTP transport on
http://ADDR/mcp (and an unauthenticated liveness probe on /healthz):
  MCP_BEARER_TOKEN=... evo mcp --http :8080
  claude mcp add --transport http evo http://localhost:8080/mcp \
    --header "Authorization: Bearer $MCP_BEARER_TOKEN"

HTTP authentication (--auth):
  bearer  clients send "Authorization: Bearer <token>"; the token is taken
          from $MCP_BEARER_TOKEN, and the server refuses to start without it
  none    no authentication; only allowed on a loopback address
          (--http 127.0.0.1:8080)

Over HTTP, k8s_kubeconfig with an embedded token, cert_private_key and
secret_value are disabled, and credential fields (passwords, keys, tokens) in
other results are masked, unless --allow-secret-export is set. Arguments that
reach the server's host are confined: deploy's compose_file to the working
directory, k8s_kubectl to built-in commands without host flags (--kubeconfig,
-f, ...), k8s_helm to remote charts on public https/oci hosts.
k8s_kubectl/k8s_apply/k8s_helm answer with an error when kubectl/helm is not
installed (the Docker image has neither). At most 32 tool calls run at once
and a batch holds at most 100 messages. A tool call is cancelled when its
client closes the connection, deletes the session (DELETE /mcp) or sends
notifications/cancelled, and when the server stops; there is no timeout. One log line per tool call (name,
status, duration; never the arguments) goes to stderr.

Cloud.ru credentials and project parameters are the server's own, resolved
exactly as for the regular CLI commands (environment, profile from
'evo configure', .evo.yaml); clients never send them.
```

```
evo mcp [flags]
```

### Options

```
      --allow-secret-export   HTTP mode: allow k8s_kubeconfig with an embedded token, cert_private_key, secret_value and unmasked credentials in results
      --auth string           HTTP authentication: bearer (token from $MCP_BEARER_TOKEN) or none (loopback address only) (default "bearer")
  -h, --help                  help for mcp
      --http string           Serve MCP over Streamable HTTP on this address (e.g. :8080) instead of stdio
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs

## evo pg

Managed PostgreSQL clusters, databases, users, backups

```
evo pg [flags]
```

### Options

```
  -h, --help   help for pg
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs
* [evo pg backup-create](#evo-pg-backup-create)	 - Start a manual backup of a PostgreSQL cluster (async).
* [evo pg backups](#evo-pg-backups)	 - List backups of a PostgreSQL cluster (name, status, size, timestamps).
* [evo pg create](#evo-pg-create)	 - Create a Managed PostgreSQL cluster.
* [evo pg databases](#evo-pg-databases)	 - List databases in a PostgreSQL cluster (name, owner, size).
* [evo pg db-create](#evo-pg-db-create)	 - Create a database in a PostgreSQL cluster.
* [evo pg db-delete](#evo-pg-db-delete)	 - Drop a database in a PostgreSQL cluster.
* [evo pg delete](#evo-pg-delete)	 - Delete a Managed PostgreSQL cluster by id (from pg_list) AND its data.
* [evo pg get](#evo-pg-get)	 - Get full details of one Managed PostgreSQL cluster by id (from pg_list): version, instances, storag…
* [evo pg list](#evo-pg-list)	 - List Managed PostgreSQL clusters in the project (id, name, status, version, health).
* [evo pg operation](#evo-pg-operation)	 - Poll a Managed PostgreSQL async operation by id (returned by pg_create/pg_delete/pg_backup_create).
* [evo pg specs](#evo-pg-specs)	 - List Managed PostgreSQL specifications (id, cpu, memory_gb, deployment_mode standard|business, min_…
* [evo pg user-create](#evo-pg-user-create)	 - Create a user in a PostgreSQL cluster.
* [evo pg user-delete](#evo-pg-user-delete)	 - Delete a user in a PostgreSQL cluster.
* [evo pg users](#evo-pg-users)	 - List users of a PostgreSQL cluster (name, granted roles).
* [evo pg versions](#evo-pg-versions)	 - List available Managed PostgreSQL versions (e.g. 15/16/17).

## evo pg backup-create

Start a manual backup of a PostgreSQL cluster (async).

### Synopsis

```text
Start a manual backup of a PostgreSQL cluster (async). Mutation: requires confirmed=true after the user approves.

Runs the pg_backup_create tool (see 'evo tools describe pg_backup_create').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo pg backup-create <cluster-id> <name> [flags]
```

### Options

```
  -h, --help   help for backup-create
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg backups

List backups of a PostgreSQL cluster (name, status, size, timestamps).

### Synopsis

```text
List backups of a PostgreSQL cluster (name, status, size, timestamps). Read-only.

Runs the pg_backups tool (see 'evo tools describe pg_backups').
```

```
evo pg backups <cluster-id> [flags]
```

### Options

```
  -h, --help   help for backups
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg create

Create a Managed PostgreSQL cluster.

### Synopsis

```text
Create a Managed PostgreSQL cluster. Resolves version (default: latest), specification (by specification_id OR cpu+ram), storage (default: spec minimum), and subnet (default: first project subnet). Without confirmed=true returns a preview only. Mutation (BILLABLE, async ~minutes): requires confirmed=true after the user approves. Poll the returned operation_id with pg_operation.

Runs the pg_create tool (see 'evo tools describe pg_create').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo pg create <name> [flags]
```

### Options

```
      --backup-retention-days int   Retention for automatic backups in days (default 14, max 30).
      --backup-schedule string      CRON schedule for automatic backups (default '0 3 * * 0' = weekly Sun 03:00; min once per 6h).
      --cpu int                     vCPU count — used to auto-pick a specification when specification_id is omitted.
      --description string          Optional cluster description.
  -h, --help                        help for create
      --initial-database string     Name of the default database to create. Default 'defaultdb'.
      --instances int               Number of nodes (1-2 standard, 1-3 business). Default 1.
      --no-backup                   Disable automatic backups. Backups are ENABLED by default and CANNOT be toggled after creation; a cluster without backups rejects pg_backup_create/pg_backups with 'backup disabled'.
      --ram int                     RAM in GB — used with cpu to auto-pick a specification.
      --specification-id string     Specification UUID (from pg_specs). Optional if cpu+ram given.
      --storage-gb int              Main data disk size in GB. Optional; default = specification minimum.
      --subnet-id string            Subnet UUID (defines the AZ). Optional; default = first project subnet.
      --version string              PostgreSQL version (from pg_versions). Optional; default = latest.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg databases

List databases in a PostgreSQL cluster (name, owner, size).

### Synopsis

```text
List databases in a PostgreSQL cluster (name, owner, size). Read-only.

Runs the pg_databases tool (see 'evo tools describe pg_databases').
```

```
evo pg databases <cluster-id> [flags]
```

### Options

```
  -h, --help   help for databases
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg db-create

Create a database in a PostgreSQL cluster.

### Synopsis

```text
Create a database in a PostgreSQL cluster. Mutation: requires confirmed=true after the user approves.

Runs the pg_db_create tool (see 'evo tools describe pg_db_create').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo pg db-create <cluster-id> <name> [flags]
```

### Options

```
  -h, --help                help for db-create
      --lc-collate string   Collation locale (optional, default C).
      --lc-ctype string     Character-set locale (optional, default C).
      --owner string        Owning user (from pg_users). Required.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg db-delete

Drop a database in a PostgreSQL cluster.

### Synopsis

```text
Drop a database in a PostgreSQL cluster. Mutation: requires confirmed=true after the user approves.

Runs the pg_db_delete tool (see 'evo tools describe pg_db_delete').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo pg db-delete <cluster-id> <name> [flags]
```

### Options

```
  -h, --help   help for db-delete
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg delete

Delete a Managed PostgreSQL cluster by id (from pg_list) AND its data.

### Synopsis

```text
Delete a Managed PostgreSQL cluster by id (from pg_list) AND its data. Mutation (async): requires confirmed=true after the user approves.

Runs the pg_delete tool (see 'evo tools describe pg_delete').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo pg delete <cluster-id> [flags]
```

### Options

```
  -h, --help        help for delete
      --id string   Cluster UUID (from pg_list). Required (alias: cluster_id).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg get

Get full details of one Managed PostgreSQL cluster by id (from pg_list): version, instances, storag…

### Synopsis

```text
Get full details of one Managed PostgreSQL cluster by id (from pg_list): version, instances, storage, connection_string, subnets, status. Read-only.

Runs the pg_get tool (see 'evo tools describe pg_get').
```

```
evo pg get <cluster-id> [flags]
```

### Options

```
  -h, --help        help for get
      --id string   PostgreSQL cluster UUID (from pg_list). Required (alias: cluster_id).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg list

List Managed PostgreSQL clusters in the project (id, name, status, version, health).

### Synopsis

```text
List Managed PostgreSQL clusters in the project (id, name, status, version, health). Read-only. Managed PostgreSQL is Cloud.ru's managed database service (postgresql.api.cloud.ru).

Runs the pg_list tool (see 'evo tools describe pg_list').
```

```
evo pg list [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg operation

Poll a Managed PostgreSQL async operation by id (returned by pg_create/pg_delete/pg_backup_create).

### Synopsis

```text
Poll a Managed PostgreSQL async operation by id (returned by pg_create/pg_delete/pg_backup_create). Returns {done, error}. Read-only.

Runs the pg_operation tool (see 'evo tools describe pg_operation').
```

```
evo pg operation <operation-id> [flags]
```

### Options

```
  -h, --help               help for operation
      --wait-seconds int   Optional: poll up to N seconds (capped 300) with backoff and return the FINAL state in ONE call — instead of the agent re-checking every few seconds. Omit for a single instant check.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg specs

List Managed PostgreSQL specifications (id, cpu, memory_gb, deployment_mode standard|business, min_…

### Synopsis

```text
List Managed PostgreSQL specifications (id, cpu, memory_gb, deployment_mode standard|business, min_storage_gb) for a version. Read-only. Use to choose specification_id for pg_create.

Runs the pg_specs tool (see 'evo tools describe pg_specs').
```

```
evo pg specs [flags]
```

### Options

```
  -h, --help             help for specs
      --version string   Product version (e.g. '17'); optional — filters specs to that version.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg user-create

Create a user in a PostgreSQL cluster.

### Synopsis

```text
Create a user in a PostgreSQL cluster. Mutation: requires confirmed=true after the user approves. The password is never echoed back.

Runs the pg_user_create tool (see 'evo tools describe pg_user_create').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo pg user-create <cluster-id> <name> [flags]
```

### Options

```
  -h, --help              help for user-create
      --password string   User password. Required.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg user-delete

Delete a user in a PostgreSQL cluster.

### Synopsis

```text
Delete a user in a PostgreSQL cluster. Mutation: requires confirmed=true after the user approves.

Runs the pg_user_delete tool (see 'evo tools describe pg_user_delete').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo pg user-delete <cluster-id> <name> [flags]
```

### Options

```
  -h, --help   help for user-delete
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg users

List users of a PostgreSQL cluster (name, granted roles).

### Synopsis

```text
List users of a PostgreSQL cluster (name, granted roles). Read-only.

Runs the pg_users tool (see 'evo tools describe pg_users').
```

```
evo pg users <cluster-id> [flags]
```

### Options

```
  -h, --help   help for users
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo pg versions

List available Managed PostgreSQL versions (e.g. 15/16/17).

### Synopsis

```text
List available Managed PostgreSQL versions (e.g. 15/16/17). Read-only. Use before pg_create.

Runs the pg_versions tool (see 'evo tools describe pg_versions').
```

```
evo pg versions [flags]
```

### Options

```
  -h, --help   help for versions
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL clusters, databases, users, backups

## evo s3

Object Storage: buckets and objects

```
evo s3 [flags]
```

### Options

```
  -h, --help   help for s3
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs
* [evo s3 bucket-create](#evo-s3-bucket-create)	 - Create an Object Storage (S3) bucket.
* [evo s3 bucket-delete](#evo-s3-bucket-delete)	 - Delete an EMPTY S3 bucket.
* [evo s3 buckets](#evo-s3-buckets)	 - List Object Storage (S3) buckets.
* [evo s3 get](#evo-s3-get)	 - Download an S3 object.
* [evo s3 ls](#evo-s3-ls)	 - List objects in an S3 bucket (key, size, last_modified).
* [evo s3 put](#evo-s3-put)	 - Upload an S3 object from inline content (content) or base64 (content_base64).
* [evo s3 rm](#evo-s3-rm)	 - Delete an S3 object.

## evo s3 bucket-create

Create an Object Storage (S3) bucket.

### Synopsis

```text
Create an Object Storage (S3) bucket. Use this when a bucket doesn't exist yet (s3_put needs an existing bucket). Idempotent — a bucket you already own is not an error. Mutation: requires confirmed=true after the user approves.

Runs the s3_bucket_create tool (see 'evo tools describe s3_bucket_create').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo s3 bucket-create <bucket> [flags]
```

### Options

```
  -h, --help               help for bucket-create
      --s3-region string   Region (optional; defaults to the configured S3 region, e.g. ru-central-1).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: buckets and objects

## evo s3 bucket-delete

Delete an EMPTY S3 bucket.

### Synopsis

```text
Delete an EMPTY S3 bucket. Destructive mutation: requires confirmed=true after the user approves.

Runs the s3_bucket_delete tool (see 'evo tools describe s3_bucket_delete').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo s3 bucket-delete <bucket> [flags]
```

### Options

```
  -h, --help   help for bucket-delete
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: buckets and objects

## evo s3 buckets

List Object Storage (S3) buckets.

### Synopsis

```text
List Object Storage (S3) buckets. Read-only. s3.cloud.ru (S3-compatible).

Runs the s3_buckets tool (see 'evo tools describe s3_buckets').
```

```
evo s3 buckets [flags]
```

### Options

```
  -h, --help   help for buckets
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: buckets and objects

## evo s3 get

Download an S3 object.

### Synopsis

```text
Download an S3 object. Returns text inline (utf-8) when small & valid, else base64. Read-only.

Runs the s3_get tool (see 'evo tools describe s3_get').
```

```
evo s3 get <bucket> <key> [flags]
```

### Options

```
  -h, --help   help for get
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: buckets and objects

## evo s3 ls

List objects in an S3 bucket (key, size, last_modified).

### Synopsis

```text
List objects in an S3 bucket (key, size, last_modified). Read-only.

Runs the s3_ls tool (see 'evo tools describe s3_ls').
```

```
evo s3 ls <bucket> [flags]
```

### Options

```
  -h, --help            help for ls
      --max-keys int    Max objects to return (default 1000).
      --prefix string   Key prefix filter (optional).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: buckets and objects

## evo s3 put

Upload an S3 object from inline content (content) or base64 (content_base64).

### Synopsis

```text
Upload an S3 object from inline content (content) or base64 (content_base64). Mutation (overwrites if key exists): requires confirmed=true after the user approves.

Runs the s3_put tool (see 'evo tools describe s3_put').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo s3 put <bucket> <key> [flags]
```

### Options

```
      --content string          Inline text content to upload.
      --content-base64 string   Base64-encoded binary content (alternative to content).
      --content-type string     MIME type (default application/octet-stream).
  -h, --help                    help for put
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: buckets and objects

## evo s3 rm

Delete an S3 object.

### Synopsis

```text
Delete an S3 object. Destructive mutation: requires confirmed=true after the user approves.

Runs the s3_rm tool (see 'evo tools describe s3_rm').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo s3 rm <bucket> <key> [flags]
```

### Options

```
  -h, --help   help for rm
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: buckets and objects

## evo secret

Secret Manager

```
evo secret [flags]
```

### Options

```
  -h, --help   help for secret
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs
* [evo secret add-version](#evo-secret-add-version)	 - Add a new value version to an existing secret (rotation).
* [evo secret create](#evo-secret-create)	 - Store a new secret at a path (value is Base64-encoded internally; never echoed back).
* [evo secret delete](#evo-secret-delete)	 - Delete a secret by path AND all its versions.
* [evo secret get](#evo-secret-get)	 - Get one secret's metadata by path (from secret_list): folder, labels, versions, KMS key.
* [evo secret list](#evo-secret-list)	 - List secrets in the project (id, name, path, description) from Cloud.ru Secret Management.
* [evo secret value](#evo-secret-value)	 - Retrieve the DECRYPTED value of a secret by path.

## evo secret add-version

Add a new value version to an existing secret (rotation).

### Synopsis

```text
Add a new value version to an existing secret (rotation). Mutation: requires confirmed=true after the user approves.

Runs the secret_add_version tool (see 'evo tools describe secret_add_version').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo secret add-version <path> [flags]
```

### Options

```
  -h, --help           help for add-version
      --value string   New secret value (plaintext). Required.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo secret](#evo-secret)	 - Secret Manager

## evo secret create

Store a new secret at a path (value is Base64-encoded internally; never echoed back).

### Synopsis

```text
Store a new secret at a path (value is Base64-encoded internally; never echoed back). Path 'folder/name' places it in a folder. Mutation: requires confirmed=true after the user approves.

Runs the secret_create tool (see 'evo tools describe secret_create').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo secret create <path> [flags]
```

### Options

```
      --description string   Optional description.
  -h, --help                 help for create
      --kms-key-id string    Optional KMS key id for encryption.
      --value string         Secret value (plaintext; stored encrypted). Required.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo secret](#evo-secret)	 - Secret Manager

## evo secret delete

Delete a secret by path AND all its versions.

### Synopsis

```text
Delete a secret by path AND all its versions. Set force=true to delete one that is in use. Mutation: requires confirmed=true after the user approves.

Runs the secret_delete tool (see 'evo tools describe secret_delete').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo secret delete <path> [flags]
```

### Options

```
      --force   Delete even if the secret is in use by other resources.
  -h, --help    help for delete
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo secret](#evo-secret)	 - Secret Manager

## evo secret get

Get one secret's metadata by path (from secret_list): folder, labels, versions, KMS key.

### Synopsis

```text
Get one secret's metadata by path (from secret_list): folder, labels, versions, KMS key. Read-only — does NOT return the value.

Runs the secret_get tool (see 'evo tools describe secret_get').
```

```
evo secret get <path> [flags]
```

### Options

```
  -h, --help   help for get
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo secret](#evo-secret)	 - Secret Manager

## evo secret list

List secrets in the project (id, name, path, description) from Cloud.ru Secret Management.

### Synopsis

```text
List secrets in the project (id, name, path, description) from Cloud.ru Secret Management. Read-only — does NOT return values. secretmanager.api.cloud.ru (v2).

Runs the secret_list tool (see 'evo tools describe secret_list').
```

```
evo secret list [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo secret](#evo-secret)	 - Secret Manager

## evo secret value

Retrieve the DECRYPTED value of a secret by path.

### Synopsis

```text
Retrieve the DECRYPTED value of a secret by path. Sensitive — returns the plaintext value; never store it in logs or files. Read-only.

Runs the secret_value tool (see 'evo tools describe secret_value').
```

```
evo secret value <path> [flags]
```

### Options

```
  -h, --help          help for value
      --version int   Specific version number (default: latest).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo secret](#evo-secret)	 - Secret Manager

## evo status

Show current deployment status

```
evo status [flags]
```

### Options

```
  -h, --help   help for status
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs

## evo tools

List, describe and call any of the evo tools (the same ones the MCP server exposes)

### Synopsis

```text
Every operation the MCP server exposes is available here as a tool.

  evo tools list [--group vm]
  evo tools describe vm_get
  evo tools call vm_get --arg name=web

Tools that create paid resources or delete data ask for confirmation the same
way as the other commands: a prompt on a terminal, --yes to confirm in
advance, --dry-run to see the preview only; without a terminal and without
--yes the call stops with exit code 3.
```

```
evo tools [flags]
```

### Options

```
  -h, --help   help for tools
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs
* [evo tools call](#evo-tools-call)	 - Call a tool and print its JSON result
* [evo tools describe](#evo-tools-describe)	 - Show a tool's description and arguments
* [evo tools list](#evo-tools-list)	 - List the tools: name, group, whether it changes anything, description

## evo tools call

Call a tool and print its JSON result

### Synopsis

```text
Call a tool and print its JSON result as is.

Arguments come from --args-file, then --args-json, then --arg (later ones
win). --arg values are converted by the tool's schema: integer, number and
boolean are parsed; an array is a comma-separated list (or a JSON array);
an object is JSON. Names and types from the JSON sources are checked
against the schema too; null means "not given".

  evo tools call list_zones
  evo tools call vm_get --arg name=web
  evo tools call open_ports --arg security_group=web-sg --arg ports=80,443 --yes
  evo tools call pg_operation --arg operation_id=OP --wait

Exit codes: 0 success; 3 confirmation not given; 4 the tool reported that
something was not found; 5 any other error reported by the tool.
```

```
evo tools call <name> [flags]
```

### Options

```
      --arg stringArray    Tool argument as name=value (repeatable)
      --args-file string   Read tool arguments from a JSON file
      --args-json string   Tool arguments as a JSON object
  -h, --help               help for call
      --wait               For tools that take wait_seconds (*_operation, stack_status): wait for the final state, polling with backoff up to the tool's limit
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo tools](#evo-tools)	 - List, describe and call any of the evo tools (the same ones the MCP server exposes)

## evo tools describe

Show a tool's description and arguments

```
evo tools describe <name> [flags]
```

### Options

```
  -h, --help   help for describe
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo tools](#evo-tools)	 - List, describe and call any of the evo tools (the same ones the MCP server exposes)

## evo tools list

List the tools: name, group, whether it changes anything, description

```
evo tools list [flags]
```

### Options

```
      --group string   Show only this group (ar, audit, billing, ca, cert, deploy, disk, dns, iam, image, k8s, kafka, monitoring, nlb, org, pg, redis, s3, secret, snat, stack, vm, vpc)
  -h, --help           help for list
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo tools](#evo-tools)	 - List, describe and call any of the evo tools (the same ones the MCP server exposes)

## evo verify

Run health checks against the deployed application (exit 1 if unhealthy)

```
evo verify [flags]
```

### Options

```
  -h, --help   help for verify
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs

## evo version

Print the version, commit, build date, Go version and platform

```
evo version [flags]
```

### Options

```
  -h, --help   help for version
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs

## evo vm

Virtual machines

```
evo vm [flags]
```

### Options

```
  -h, --help   help for vm
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs
* [evo vm create](#evo-vm-create)	 - Provision a bare Cloud.ru VM.
* [evo vm delete](#evo-vm-delete)	 - Permanently delete a VM and all data on it (irreversible).
* [evo vm flavors](#evo-vm-flavors)	 - List available Cloud.ru VM flavors (name, vCPU, RAM, free-tier flag) with a monthly cost estimate e…
* [evo vm get](#evo-vm-get)	 - Get LIVE details for one VM by vm_id or name: its PRIVATE IP (private_ip), any public/floating IP (…
* [evo vm list](#evo-vm-list)	 - List the project's VMs from the LIVE API: id, name, state, availability zone and creation time.
* [evo vm zones](#evo-vm-zones)	 - List available Cloud.ru availability zones (UUID + name).

## evo vm create

Provision a bare Cloud.ru VM.

### Synopsis

```text
Provision a bare Cloud.ru VM. By default assigns a public floating IP. Set no_public_ip=true for a PRIVATE VM (subnet-only, interface type 'regular') — reachable only inside the VPC and the valid target type for a load balancer. Attaches a subnet + security group automatically (required — omitting the interface 500s). For docker-compose deployments, use 'deploy' instead. Mutation (billable): requires confirmed=true after the user approves; without it returns a preview only.

Runs the provision tool (see 'evo tools describe provision').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo vm create <name> [flags]
```

### Options

```
      --disk-size int      Boot disk size in GB (default: 10, raised to the image's min_disk).
      --flavor string      VM flavor name or UUID (e.g. 'lowcost10-2-4'). Default: auto (smallest lowcost, not free-tier).
  -h, --help               help for create
      --image-id string    Boot image UUID (see list_images).
      --no-public-ip       Create a PRIVATE VM (no floating IP, interface type 'regular'). Use for load-balancer targets / internal-only nodes.
      --subnet-id string   Subnet UUID for the interface (default: a subnet in the target zone).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vm](#evo-vm)	 - Virtual machines

## evo vm delete

Permanently delete a VM and all data on it (irreversible).

### Synopsis

```text
Permanently delete a VM and all data on it (irreversible). ALWAYS requires confirmed=true (user approval). With no arguments, deletes the current local deployment. To delete a SPECIFIC VM (e.g. an orphan from a failed deploy), pass vm_id or name. By default the VM's floating IP is released too (release_ip). The security group rules its deploy/deploy_apply created are deleted after the VM, unless another deployment in the local state still needs the port; a rule that cannot be deleted is reported and retried by the next destroy.

Runs the destroy tool (see 'evo tools describe destroy').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo vm delete <vm-id|name> [flags]
```

### Options

```
  -h, --help         help for delete
      --release-ip   Also release the VM's floating IP (default: true).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vm](#evo-vm)	 - Virtual machines

## evo vm flavors

List available Cloud.ru VM flavors (name, vCPU, RAM, free-tier flag) with a monthly cost estimate e…

### Synopsis

```text
List available Cloud.ru VM flavors (name, vCPU, RAM, free-tier flag) with a monthly cost estimate each. Use a flavor name as the 'flavor' argument for deploy_plan in manual mode.

Runs the list_flavors tool (see 'evo tools describe list_flavors').
```

```
evo vm flavors [flags]
```

### Options

```
  -h, --help   help for flavors
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vm](#evo-vm)	 - Virtual machines

## evo vm get

Get LIVE details for one VM by vm_id or name: its PRIVATE IP (private_ip), any public/floating IP (…

### Synopsis

```text
Get LIVE details for one VM by vm_id or name: its PRIVATE IP (private_ip), any public/floating IP (public_ip), state, and every network interface. Use this whenever you need a VM's IP — e.g. to wire a private service endpoint into a config or secret after creating the VM. Unlike 'list'/'status' (which read local state and omit the private IP), this calls the live API. Never shell out to curl/ssh to discover a VM's IP — use this tool.

Runs the vm_get tool (see 'evo tools describe vm_get').
```

```
evo vm get <vm-id|name> [flags]
```

### Options

```
  -h, --help   help for get
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vm](#evo-vm)	 - Virtual machines

## evo vm list

List the project's VMs from the LIVE API: id, name, state, availability zone and creation time.

### Synopsis

```text
List the project's VMs from the LIVE API: id, name, state, availability zone and creation time. Read-only. For IPs and interfaces of one VM use vm_get. Unlike 'list' (local deployment records of this directory), this shows every VM in the project.

Runs the vm_list tool (see 'evo tools describe vm_list').
```

```
evo vm list [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vm](#evo-vm)	 - Virtual machines

## evo vm zones

List available Cloud.ru availability zones (UUID + name).

### Synopsis

```text
List available Cloud.ru availability zones (UUID + name). Use a zone UUID as the 'region' argument for deploy or provision.

Runs the list_zones tool (see 'evo tools describe list_zones').
```

```
evo vm zones [flags]
```

### Options

```
  -h, --help   help for zones
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vm](#evo-vm)	 - Virtual machines

## evo vpc

Networks: VPCs, routes, subnets, security groups, floating IPs

```
evo vpc [flags]
```

### Options

```
  -h, --help   help for vpc
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs
* [evo vpc create](#evo-vpc-create)	 - Create a new VPC (virtual private cloud) (async).
* [evo vpc delete](#evo-vpc-delete)	 - Delete a VPC by id (from vpc_list) (async).
* [evo vpc fip-attach](#evo-vpc-fip-attach)	 - Bind an existing FREE floating IP to a VM's primary interface, giving it a public address.
* [evo vpc fip-release](#evo-vpc-fip-release)	 - Release (delete) a floating IP, freeing it from the project.
* [evo vpc fips](#evo-vpc-fips)	 - List Cloud.ru floating IPs (address, state, whether available to attach).
* [evo vpc get](#evo-vpc-get)	 - Get full details of one VPC by id (from vpc_list).
* [evo vpc list](#evo-vpc-list)	 - List Evolution VPCs (virtual private clouds) in the project (id, name, default, type).
* [evo vpc operation](#evo-vpc-operation)	 - Poll a VPC async operation by id (from vpc_create/delete, vpc_route_create/delete).
* [evo vpc route-create](#evo-vpc-route-create)	 - Add a static route to a VPC (async).
* [evo vpc route-delete](#evo-vpc-route-delete)	 - Delete a static route from a VPC by id (from vpc_routes) (async).
* [evo vpc routes](#evo-vpc-routes)	 - List static routes of a VPC (id, subnet, next_hop_type, status).
* [evo vpc security-groups](#evo-vpc-security-groups)	 - List Cloud.ru security groups (UUID, name, availability zone).
* [evo vpc subnets](#evo-vpc-subnets)	 - List Cloud.ru subnets (UUID, name, availability zone).

## evo vpc create

Create a new VPC (virtual private cloud) (async).

### Synopsis

```text
Create a new VPC (virtual private cloud) (async). Mutation: requires confirmed=true after the user approves.

Runs the vpc_create tool (see 'evo tools describe vpc_create').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo vpc create <name> [flags]
```

### Options

```
      --description string   Optional description.
  -h, --help                 help for create
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo vpc delete

Delete a VPC by id (from vpc_list) (async).

### Synopsis

```text
Delete a VPC by id (from vpc_list) (async). Its subnets/resources must be removed first. Mutation: requires confirmed=true after the user approves.

Runs the vpc_delete tool (see 'evo tools describe vpc_delete').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo vpc delete <id> [flags]
```

### Options

```
  -h, --help   help for delete
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo vpc fip-attach

Bind an existing FREE floating IP to a VM's primary interface, giving it a public address.

### Synopsis

```text
Bind an existing FREE floating IP to a VM's primary interface, giving it a public address. Use when a VM has no public_ip — e.g. provisioning from a large custom image outran the wizard's wait and it returned before assigning one. Pick a free address from list_floating_ips. Mutation: requires confirmed=true.

Runs the fip_attach tool (see 'evo tools describe fip_attach').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo vpc fip-attach <fip> [flags]
```

### Options

```
  -h, --help        help for fip-attach
      --vm string   VM name or UUID to attach it to.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo vpc fip-release

Release (delete) a floating IP, freeing it from the project.

### Synopsis

```text
Release (delete) a floating IP, freeing it from the project. Identify it by 'id' or 'ip_address'. REFUSES to release an IP that is currently in use by a VM (detach or destroy the VM first). Mutation: requires confirmed=true after the user approves.

Runs the release_floating_ip tool (see 'evo tools describe release_floating_ip').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo vpc fip-release [id] [flags]
```

### Options

```
  -h, --help                help for fip-release
      --ip-address string   Floating IP address, e.g. '213.171.25.19' (alternative to id).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo vpc fips

List Cloud.ru floating IPs (address, state, whether available to attach).

### Synopsis

```text
List Cloud.ru floating IPs (address, state, whether available to attach). Read-only.

Runs the list_floating_ips tool (see 'evo tools describe list_floating_ips').
```

```
evo vpc fips [flags]
```

### Options

```
  -h, --help   help for fips
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo vpc get

Get full details of one VPC by id (from vpc_list).

### Synopsis

```text
Get full details of one VPC by id (from vpc_list). Read-only.

Runs the vpc_get tool (see 'evo tools describe vpc_get').
```

```
evo vpc get <id> [flags]
```

### Options

```
  -h, --help   help for get
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo vpc list

List Evolution VPCs (virtual private clouds) in the project (id, name, default, type).

### Synopsis

```text
List Evolution VPCs (virtual private clouds) in the project (id, name, default, type). Read-only. vpc.api.cloud.ru.

Runs the vpc_list tool (see 'evo tools describe vpc_list').
```

```
evo vpc list [flags]
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo vpc operation

Poll a VPC async operation by id (from vpc_create/delete, vpc_route_create/delete).

### Synopsis

```text
Poll a VPC async operation by id (from vpc_create/delete, vpc_route_create/delete). Returns {done, error}. Read-only.

Runs the vpc_operation tool (see 'evo tools describe vpc_operation').
```

```
evo vpc operation <operation-id> [flags]
```

### Options

```
  -h, --help               help for operation
      --wait-seconds int   Optional: poll up to N seconds (capped 300) with backoff and return the FINAL state in ONE call — instead of re-checking every few seconds. Omit for a single instant check.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo vpc route-create

Add a static route to a VPC (async).

### Synopsis

```text
Add a static route to a VPC (async). Destination is a CIDR (subnet); the next hop is a VM interface (next_hop_vm_id+next_hop_interface_id), a VIP (next_hop_vip_id), or a Magic Router (next_hop_magic_router_id+next_hop_az). Mutation: requires confirmed=true after the user approves.

Runs the vpc_route_create tool (see 'evo tools describe vpc_route_create').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo vpc route-create <vpc-id> [flags]
```

### Options

```
      --description string                Optional route description.
  -h, --help                              help for route-create
      --next-hop-az string                Availability zone name (e.g. 'ru.AZ-1') for a Magic-Router next hop.
      --next-hop-interface-id string      VM network interface UUID.
      --next-hop-magic-router-id string   Magic Router UUID for a Magic-Router next hop (with next_hop_az).
      --next-hop-vip-id string            Virtual IP UUID for a VIP next hop.
      --next-hop-vm-id string             VM UUID for a VM-interface next hop (with next_hop_interface_id).
      --subnet string                     Destination CIDR, e.g. '192.168.0.0/24'. Required.
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo vpc route-delete

Delete a static route from a VPC by id (from vpc_routes) (async).

### Synopsis

```text
Delete a static route from a VPC by id (from vpc_routes) (async). Mutation: requires confirmed=true after the user approves.

Runs the vpc_route_delete tool (see 'evo tools describe vpc_route_delete').
Asks for confirmation: a prompt on a terminal, --yes to confirm in advance, --dry-run to see the preview only.
```

```
evo vpc route-delete <vpc-id> <route-id> [flags]
```

### Options

```
  -h, --help   help for route-delete
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo vpc routes

List static routes of a VPC (id, subnet, next_hop_type, status).

### Synopsis

```text
List static routes of a VPC (id, subnet, next_hop_type, status). Read-only.

Runs the vpc_routes tool (see 'evo tools describe vpc_routes').
```

```
evo vpc routes <vpc-id> [flags]
```

### Options

```
  -h, --help   help for routes
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo vpc security-groups

List Cloud.ru security groups (UUID, name, availability zone).

### Synopsis

```text
List Cloud.ru security groups (UUID, name, availability zone). Read-only. With rules=true each group carries its rules (id, direction, protocol, port range, source, description, state).

Runs the list_security_groups tool (see 'evo tools describe list_security_groups').
```

```
evo vpc security-groups [flags]
```

### Options

```
  -h, --help    help for security-groups
      --rules   Also list each group's rules (one more API call per group).
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo vpc subnets

List Cloud.ru subnets (UUID, name, availability zone).

### Synopsis

```text
List Cloud.ru subnets (UUID, name, availability zone). Read-only.

Runs the list_subnets tool (see 'evo tools describe list_subnets').
```

```
evo vpc subnets [flags]
```

### Options

```
  -h, --help   help for subnets
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Networks: VPCs, routes, subnets, security groups, floating IPs

## evo whoami

Show the active profile and check its credentials

### Synopsis

```text
Exchange the access key of the active profile for an IAM token and show
the profile, the key ID (masked), the project and the token expiry.

A valid token cached by an earlier command is reused; IAM is not contacted
again until it is about to expire. Exits with 2 when the credentials are
missing or rejected.
```

```
evo whoami [flags]
```

### Options

```
  -h, --help   help for whoami
```

### Options inherited from parent commands

```
      --dry-run             Show what would be created or deleted (with a cost estimate) and exit
      --json                Output in JSON format
      --no-color            Disable colored output
      --profile string      Credentials profile (default: $CLOUDRU_PROFILE or "default")
      --project-id string   Cloud.ru project UUID (default: from the profile or $CLOUDRU_PROJECT_ID)
      --region string       Availability zone UUID (default: from the profile or $CLOUDRU_REGION)
      --verbose             Enable verbose logging
  -y, --yes                 Confirm operations that create paid resources or delete data, without a prompt
```

### SEE ALSO

* [evo](#evo)	 - Deploy docker-compose projects to Cloud.ru VMs
