FROM golang:1.26.2-alpine@sha256:f85330846cde1e57ca9ec309382da3b8e6ae3ab943d2739500e08c86393a21b1 AS builder

ARG TARGETARCH

# Pin the toolchain to the version in go.mod for reproducible builds.
# Without this, GOTOOLCHAIN=auto would silently download whatever
# point release happens to be current at build time.
ENV GOTOOLCHAIN=go1.26.3

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o inveniam-mcp-server ./cmd/inveniam-mcp-server

FROM gcr.io/distroless/static-debian12@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

COPY --from=builder /build/inveniam-mcp-server /inveniam-mcp-server
COPY --from=builder /build/abi /app/abi

EXPOSE 8080
EXPOSE 9090

ENTRYPOINT ["/inveniam-mcp-server"]
CMD ["--transport", "http"]
