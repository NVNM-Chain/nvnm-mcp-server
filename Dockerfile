FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o inveniam-mcp-server ./cmd/inveniam-mcp-server

FROM gcr.io/distroless/static-debian12

COPY --from=builder /build/inveniam-mcp-server /inveniam-mcp-server

EXPOSE 8080
EXPOSE 9090

ENTRYPOINT ["/inveniam-mcp-server"]
CMD ["--transport", "http"]
