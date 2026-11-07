FROM golang:1.26.6-alpine@sha256:af8d6740070b8906d12eae1c3e3ea0957fb63f492051ea05e354c38ef9fe88df AS builder

ARG TARGETARCH

# VERSION is injected into internal/version.Version via -ldflags -X below.
# Without it the binary falls back to "dev". The image workflow passes the
# git tag (see .github/workflows/image.yml, build-args). Do not default this
# to a release-looking string: a plausible-but-wrong version is worse than an
# obviously-unset one, which is exactly how every image from rc11 onward came
# to report itself as rc10.
ARG VERSION=dev

# Pin the toolchain to the version in go.mod for reproducible builds.
# Without this, GOTOOLCHAIN=auto would silently download whatever
# point release happens to be current at build time.
ENV GOTOOLCHAIN=go1.26.6

WORKDIR /build

# Hermetic, vendored build: all dependencies are committed under vendor/, so the
# build never contacts the module proxy. This makes the image build immune to
# proxy flakes (one of which failed PR #47's image job) and keeps the dependency
# set supply-chain-deterministic -- what is reviewed in vendor/ is exactly what
# is compiled. COPY brings vendor/ in with the source; -mod=vendor forces its use
# and fails the build rather than silently falling back to the network.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -mod=vendor \
      -ldflags="-s -w -X github.com/NVNM-Chain/nvnm-mcp-server/internal/version.Version=${VERSION}" \
      -o nvnm-mcp-server ./cmd/nvnm-mcp-server

FROM gcr.io/distroless/static-debian12@sha256:d75cdd72874d4790092fcb1b058493ecf6bb5bf2b2b897045b00ff01d91843f2

COPY --from=builder /build/nvnm-mcp-server /nvnm-mcp-server
COPY --from=builder /build/abi /app/abi

EXPOSE 8080
EXPOSE 9090

# Run as the distroless nonroot user (UID/GID 65532) so the image itself
# is non-root even outside Kubernetes. The k8s securityContext also pins
# runAsUser=65532; this makes a plain `docker run` match that posture.
USER 65532:65532

ENTRYPOINT ["/nvnm-mcp-server"]
CMD ["--transport", "http"]
