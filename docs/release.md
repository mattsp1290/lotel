# Install, upgrade, and release lotel

The installer and `lotel upgrade` consume the same versioned release manifest.
The first published release is required before the public install URL works.

## Supported hosts

| Target | Build and smoke-test host | Runtime baseline |
| --- | --- | --- |
| `linux-x86_64` | Ubuntu 22.04 x86_64 | glibc 2.35 and system `libstdc++` |
| `linux-aarch64` | Ubuntu 22.04 arm64 | glibc 2.35 and system `libstdc++` |
| `macos-x86_64` | macOS 15 Intel | macOS 15 or later |
| `macos-aarch64` | macOS 15 Apple Silicon | macOS 15 or later |

Linux binaries use bundled DuckDB archives but still link system C/C++
libraries. They are not static musl binaries. Alpine and Windows are not
supported release targets. Docker must be installed and running to collect
telemetry. Installation and release checks do not require Docker.

The release workflow builds each target natively with CGO enabled. It executes
`--version` and a DuckDB query with an isolated home on each target. Local
verification on one host does not establish compatibility with the other hosts.

## Install a release

```sh
curl -fsSL https://github.com/mattsp1290/lotel/releases/latest/download/install.sh | sh
```

The default destination is `$HOME/.local/bin/lotel`. The installer needs `sh`,
`curl`, `awk`, `sed`, and one of `sha256sum`, `shasum`, or `openssl`, plus standard Unix
utilities. It does not use `sudo` or edit shell profiles. It prints a warning
if its destination is outside `PATH` or another executable shadows it.

| Variable | Purpose |
| --- | --- |
| `LOTEL_INSTALL_DIR` | Destination directory, defaults to `$HOME/.local/bin` |
| `LOTEL_INSTALL_VERSION` | A published stable tag such as `v1.2.3`; defaults to latest |
| `LOTEL_RELEASE_BASE_URL` | HTTPS mirror or local absolute `file://` release tree |

When using a pipe, assign installer variables to the shell that runs it:

```sh
curl -fsSL https://github.com/mattsp1290/lotel/releases/latest/download/install.sh | \
  LOTEL_INSTALL_DIR="$HOME/bin" LOTEL_INSTALL_VERSION=v1.2.3 sh
```

Installation verifies the manifest schema, target URL, checksum, and candidate
`--version` output before an atomic rename in the destination directory.
The candidate version check has a ten-second deadline on macOS and Linux.
A handled failure leaves the previous executable in place and removes the
staged files. An uncatchable process kill can leave temporary files behind.

Use the installer to bootstrap lotel. Use `lotel upgrade` to update an existing
installation with Collector restart coordination. The standalone installer does
not manage the running Collector.

## Upgrade and expect a collection gap

```sh
lotel upgrade --check
lotel upgrade --check --json
lotel upgrade
lotel upgrade --version v1.2.3
```

`--check` fetches and validates metadata, reports the target, and exits 0 when
an update exists. It never changes the executable or contacts the Collector.
Plain upgrades skip equal or older releases. `--version` selects a stable tag
and permits a deliberate rollback. Neither path opens the telemetry database.

Before any Collector interruption, the updater downloads the candidate beside
the resolved executable, verifies its SHA-256, and checks its version with a
bounded subprocess. Invalid downloads never stop collection.

For an actual replacement, the CLI checks whether the Collector is running.
If it is running, the CLI warns agents on stderr that OpenTelemetry collection
will be unavailable, stops the Collector, replaces the executable, then runs
`start --wait --config <recorded-config-path>` through the new executable.
The new version can therefore update the Collector image and managed defaults.
Restart preserves the recorded configuration choice even when upgrade runs
from another directory. Custom configuration files remain untouched.

Agents must treat the interval between the stop warning and healthy restart as
a telemetry gap. Do not run application checks that require complete telemetry
during this interval. The CLI reports when collection resumes. It preserves an
initially stopped Collector. Checks, no-op upgrades, and validation failures do
not stop or restart it. A replacing upgrade requires Docker inspection to
succeed. A daemon, permission, or inspection failure aborts before replacement
and preserves the executable and recorded Collector state.

If stopping or replacement fails after the stop begins, the CLI attempts to
restart through the executable still at the installation path. Recovery uses
a fresh bounded context even if the original request was cancelled. A failed
restart returns nonzero and prints a `start --wait` recovery command. The
installed executable can already be new when restart fails. Keep that in mind
before retrying or rolling back. Forced termination or power loss can prevent
automatic recovery.

The JSON report contains `current`, `latest`, `target`, `path`,
`update_available`, `upgraded`, and `collector_restarted`. With a pinned version,
`latest` and `target` both describe that selected release. `update_available`
means the selected release is newer than the current version. A pinned rollback
can report `upgraded=true` and `update_available=false`. Operational errors exit
nonzero. Warnings, Collector command output, and recovery advice use stderr.

Source builds default to a `source` provenance marker. They require `--force`
for replacement, even when a version is pinned. Rebuild those installations
with `make install` instead. Release builds carry a separate `release` marker.
An uncomparable development version also requires `--force` or an explicit tag.
No command checks for updates automatically.

## Manifest contract

A release publishes these six files:

- `lotel-linux-x86_64`.
- `lotel-linux-aarch64`.
- `lotel-macos-x86_64`.
- `lotel-macos-aarch64`.
- `lotel-manifest.json`.
- `install.sh`.

The raw binaries report `lotel vX.Y.Z`. The schema-1 manifest contains
`schema`, `version`, `tag`, `assets`, and `sha256`. `version` is `X.Y.Z` and
`tag` is `vX.Y.Z`, with no leading zeroes. Each asset URL must exactly equal
`<base>/download/<tag>/lotel-<target>`. Each digest is 64 lowercase hex digits.
Both consumers reject incompatible schemas, mismatched tags, unexpected URLs,
and invalid digests. HTTPS downloads may follow only HTTPS redirects.

The installer reads the format generated by `distribution/manifest.py`.
Two-space indentation and one key per line are part of this shell parser's
contract. Do not hand-format published manifests.

For latest, consumers read `<base>/latest/download/lotel-manifest.json`.
For a pin, they read `<base>/download/<tag>/lotel-manifest.json`.
The default base is `https://github.com/mattsp1290/lotel/releases`.
An HTTPS mirror must preserve this layout. Local `file://` trees support
offline tests. Checksums detect corruption. They do not authenticate a release
if its origin and manifest are compromised together.

## Build and test from source

```sh
make build
make check
make install
```

Go 1.24.4 or later, CGO, and a native C/C++ toolchain are required.
`make build` derives the version from Git; `VERSION=v1.2.3` overrides it.
`PREFIX` selects the source install prefix, defaults to `$HOME/.local`, and
`DESTDIR` stages it. Direct `go build` reports `dev`.

`make check` runs Go tests, builds, vet, and the executable installer and upgrade
journeys. The journeys use local release fixtures and isolated homes. They use
a fake Docker executable and a test health server to check restart behavior
without touching an existing Collector.

## Publish a release

An approved annotated stable `vX.Y.Z` tag on `main` triggers
`.github/workflows/release.yml`. The workflow requires a new version greater
than every published stable release. It builds and verifies all four native
targets, generates the manifest, and publishes only after those checks pass.

First run a package-only dry run from the merged code:

```sh
gh workflow run release.yml -f version=v0.0.0
```

A manual dispatch uploads workflow artifacts and never publishes a release.
After review of the dry run, the repository owner can create and push an
annotated version tag. Publishing a release is separate from opening the
installation PR.

```sh
git tag -a vX.Y.Z -m "lotel vX.Y.Z"
git push origin vX.Y.Z
```

After publication, test the public install URL and `lotel upgrade --check --json`
on each target. If a tag run fails, fix the code and use a new version. Do not
move published tags. No release has been published by the installation changes
themselves.
