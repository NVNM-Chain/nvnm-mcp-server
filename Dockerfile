FROM golang:1.26.4-alpine@sha256:f23e8b227fb4493eabe03bede4d5a32d04092da71962f1fb79b5f7d1e6c2a17f AS builder

ARG TARGETARCH

# Pin the toolchain to the version in go.mod for reproducible builds.
# Without this, GOTOOLCHAIN=auto would silently download whatever
# point release happens to be current at build time.
ENV GOTOOLCHAIN=go1.26.4

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o nvnm-mcp-server ./cmd/nvnm-mcp-server

FROM gcr.io/distroless/static-debian12@sha256:9c346e4be81b5ca7ff31a0d89eaeade58b0f95cfd3baed1f36083ddb47ca3160

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
