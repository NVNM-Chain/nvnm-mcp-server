FROM golang:1.26-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS builder

ARG TARGETARCH

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
