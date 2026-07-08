FROM golang:1.27rc2-alpine@sha256:7870fdc211100210e7380f487953c4188fcbeac99646a56926a973161a3eedcd AS builder

ARG TARGETARCH

# Pin the toolchain to the version in go.mod for reproducible builds.
# Without this, GOTOOLCHAIN=auto would silently download whatever
# point release happens to be current at build time.
ENV GOTOOLCHAIN=go1.26.5

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o nvnm-mcp-server ./cmd/nvnm-mcp-server

FROM gcr.io/distroless/static-debian12@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

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
