# The image exists for the scriptable half of octoscope, not the dashboard.
# A TUI in a container is the wrong shape — nothing is attached to read it —
# while `--json` / `--plain` fetch once, print and exit, which is exactly a
# container workload. The README documents the CI recipe.
#
# No build stage: goreleaser has already produced the static binaries by the
# time it builds this, and lays them out in the build context. Compiling
# again here would ship a binary nobody had checksummed — and goreleaser's
# own docs open with "Don't build binaries in your Dockerfile".
#
# Base chosen by measurement rather than by reputation. `FROM scratch`
# builds fine, is 0.6 MB smaller, and answers `--version` correctly — then
# fails on the first real call with:
#
#   tls: failed to verify certificate: x509: certificate signed by unknown authority
#
# because a static Go binary still needs the host's CA bundle to reach
# api.github.com, and scratch has none. That is a failure no build-time
# check catches. distroless/static carries the bundle, runs as an
# unprivileged user out of the box, and receives updates for both.
FROM gcr.io/distroless/static-debian12:nonroot

# dockers_v2 builds every platform in one buildx pass, so the binaries are
# laid out per platform rather than at the context root: TARGETPLATFORM is
# "linux/amd64" or "linux/arm64" and buildx sets it per image being built.
# Copying a bare ./octoscope instead would fail the build outright, which is
# at least the honest failure — the dangerous version of getting this wrong
# is an image that runs the other architecture's binary.
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/octoscope /octoscope

# No CMD, and no `--help` default either. Every useful invocation passes a
# flag (`--json`, `--plain`), and what a bare `docker run` actually does is
# worth stating because it is better than the alternatives — measured:
#
#   octoscope: could not open a new TTY: open /dev/tty: no such device or address
#   exit 1
#
# It refuses immediately and names the real cause. A `CMD ["--help"]` would
# be friendlier to explore and worse to depend on: help text on stdout at
# exit 0 is something a `| jq` pipeline can mistake for output, where the
# refusal above cannot.
ENTRYPOINT ["/octoscope"]
