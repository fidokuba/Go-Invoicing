# syntax=docker/dockerfile:1
#
# Milestone 11 Part 5: the production container image for ./cmd/api.
#
# This is deliberately NOT cmd/release: that tool builds all five native
# release targets and expects release-oriented Git behaviour (a resolved
# HEAD commit, a semantic version). This Dockerfile only ever needs one
# Linux binary for whatever platform Docker/BuildKit is targeting, built
# from build args a caller supplies directly — it never shells out to
# git itself, so the build context needs no .git directory at all (see
# .dockerignore). The linker-flag *shape* is intentionally identical to
# cmd/release/ldflags.go's releaseLDFlags (-s -w plus the same three -X
# targets against go-invoicing/internal/buildinfo), so both build paths
# report metadata through the exact same internal/buildinfo fields and
# the exact same `--version`/startup-log/build-info-metric machinery —
# there is only ever one build-info implementation.

# ---------------------------------------------------------------------
# Frontend builder (Milestone 12)
# ---------------------------------------------------------------------
# --platform=$BUILDPLATFORM: the frontend build produces the same
# platform-independent static assets regardless of the final image's
# target OS/architecture, so — like the Go builder below — it always
# runs natively on the build host, never under emulation.
#
# Pinned by digest (matching every other base image in this file — see
# the Go builder/runtime stages' own comments); Dependabot's existing
# "docker" ecosystem entry (.github/dependabot.yml) already covers every
# FROM in this file, this one included.
#
# Only web/ is copied in — this stage never touches the Go source tree —
# and its own package.json/package-lock.json are copied first, in their
# own layer, so an ordinary frontend source change never invalidates the
# `npm ci` layer. The build writes directly into internal/webui/dist
# (see web/vite.config.ts's build.outDir); the Go builder stage below
# copies that output over the committed placeholder before compiling.
FROM --platform=$BUILDPLATFORM node:25-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370 AS frontend-builder

WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci

COPY web/ ./
RUN npm run build

# ---------------------------------------------------------------------
# Builder
# ---------------------------------------------------------------------
# --platform=$BUILDPLATFORM: always run the Go toolchain natively on
# whatever architecture is doing the build, never under emulation — Go
# cross-compiles to the real target (see TARGETOS/TARGETARCH below)
# faster and more reliably than QEMU-emulating the compiler itself would.
#
# Pinned by digest (Milestone 11 Part 7), not just the floating
# "1.25-bookworm" tag: reproducibility for a given Dockerfile revision —
# the same source + the same digest always produces the same builder
# environment, regardless of what "1.25-bookworm" happens to point at on
# a later date. Dependabot (.github/dependabot.yml) tracks this digest's
# docker ecosystem entry and opens an ordinary, CI-reviewed PR when the
# upstream tag moves (e.g. a Debian security patch) — pinning without
# that would just freeze this image forever instead.
FROM --platform=$BUILDPLATFORM golang:1.27-bookworm@sha256:69a7b9788769bec032d238959b61854e9ae87f57be9029ec04e9885fabf99195 AS builder

WORKDIR /src

# Cache-friendly layering: download modules in their own layer, keyed
# only on go.mod/go.sum, before the rest of the source is even copied in
# — an ordinary code change (which doesn't touch go.mod/go.sum) never
# invalidates this layer or re-downloads anything.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download

COPY . .

# Overwrite the committed dist/index.html placeholder (see
# internal/webui/webui.go) with the real production frontend build —
# the ONLY thing this build ever embeds into the final binary. No
# frontend source, no node_modules, and no Node runtime itself cross
# into this stage or the runtime image below; only these already-built,
# static files do.
COPY --from=frontend-builder /src/internal/webui/dist ./internal/webui/dist

# BuildKit populates these automatically from the --platform requested
# at build time (e.g. "docker buildx build --platform linux/amd64,linux/arm64").
# A plain `docker build` with no --platform still sets them correctly for
# the host's own architecture.
ARG TARGETOS
ARG TARGETARCH

# Build metadata (Milestone 11 Part 2's internal/buildinfo). Defaults
# match internal/buildinfo's own uninjected placeholders exactly, so an
# ordinary `docker build .` with no --build-arg behaves like an ordinary
# `go build ./cmd/api` — never silently produces something that looks
# like a real release. A genuine release image (Part 6) supplies real
# values via --build-arg.
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
      -trimpath \
      -ldflags "-s -w \
        -X go-invoicing/internal/buildinfo.Version=${VERSION} \
        -X go-invoicing/internal/buildinfo.Commit=${COMMIT} \
        -X go-invoicing/internal/buildinfo.BuildTime=${BUILD_TIME}" \
      -o /out/go-invoicing \
      ./cmd/api

# ---------------------------------------------------------------------
# Runtime
# ---------------------------------------------------------------------
# distroless static, not Alpine: this application is CGO-free (proven in
# Milestone 11 Part 1), so it needs no libc at all, which is the one
# thing Alpine's musl would otherwise offer over a plain static binary —
# choosing Alpine here would only add a package manager and a shell this
# image has no use for, each its own (small but real) source of CVEs to
# track. distroless/static ships exactly what a static Go binary needs
# and nothing else: an /etc/passwd entry for a non-root user, CA
# certificates (for a production DATABASE_URL that verifies TLS against
# a managed PostgreSQL provider), and timezone data — no shell, no
# package manager, no coreutils. The :nonroot tag additionally defaults
# to UID/GID 65532 ("nonroot"); USER below is still set explicitly so
# this Dockerfile documents that fact itself rather than relying on
# knowledge of one specific tag's default.
#
# Pinned by digest (Milestone 11 Part 7) for the same reproducibility
# reason as the builder stage above — see its own comment, including why
# Dependabot (not a permanent freeze) is what keeps this current.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS runtime

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

# Standard OCI labels, derived from the exact same build args as the
# embedded internal/buildinfo values above — inspectable with
# `docker inspect` even before the container ever runs, and always
# consistent with what the running binary's own --version reports.
LABEL org.opencontainers.image.title="go-invoicing" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}"

COPY --from=builder /out/go-invoicing /go-invoicing

# 65532:65532 is distroless's well-known "nonroot" identity — spelled out
# numerically here (rather than the name "nonroot") so this is
# unambiguous even to a tool that only reads UID/GID, e.g. Kubernetes'
# runAsUser or `docker inspect`'s own numeric reporting.
USER 65532:65532

# Documentation only — EXPOSE does not publish the port or provide any
# firewalling; see compose.prod.yaml/README for actual exposure.
EXPOSE 8080

# The binary is the container's PID 1 directly (no shell wrapper is even
# possible here — distroless has no /bin/sh to wrap it with), so it
# receives SIGTERM directly on `docker stop`/Compose shutdown: Milestone
# 9's graceful-shutdown handling applies exactly as it does outside a
# container. See compose.prod.yaml's stop_grace_period for why the
# container-level grace period is set safely longer than the
# application's own 5-second shutdown timeout.
ENTRYPOINT ["/go-invoicing"]

# No Dockerfile HEALTHCHECK: this image intentionally has no shell,
# curl, or wget to run one with, and adding any of them (or a
# purpose-built healthcheck binary) solely to satisfy a HEALTHCHECK
# directive would work against the whole reason distroless was chosen.
# /health and /health/db are still fully available over the network —
# Compose, a cloud platform's own HTTP health check, or CI's container
# smoke test (.github/workflows/ci.yml) all probe them from outside the
# container instead. See README's "Self-hosted Docker Compose" section.
