FROM golang:1.26.5-alpine@sha256:99e12cfb19b753915f9b9fdc5a99f1869a24a69d3a0955832d5702e7fa68f1be AS builder

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
ENV GOTOOLCHAIN=go1.26.5

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
      -ldflags="-s -w -X github.com/NVNM-Chain/nvnm-mcp-server/internal/version.Version=${VERSION}" \
      -o nvnm-mcp-server ./cmd/nvnm-mcp-server

FROM gcr.io/distroless/static-debian12@sha256:22fd79fd75eab2372585b44517f8a094349938919dc613aafc37e4bdc9967c82

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
