package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
	apperrors "github.com/inveniam/nvnm-mcp-server/internal/errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "inveniam-mcp-server"

type requestIDKey struct{}

// RequestIDFromContext returns the request ID attached by the middleware, if any.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// NewMCPMiddleware returns MCP receiving middleware that creates an OTel span
// and records metrics for every incoming method call.
//
// Privacy: tool arguments, return values, and error messages are never recorded.
func NewMCPMiddleware(metrics *Metrics, logger *slog.Logger) mcp.Middleware {
	tracer := otel.Tracer(tracerName)

	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			reqID := uuid.New().String()
			ctx = context.WithValue(ctx, requestIDKey{}, reqID)

			toolName := extractToolName(method, req)
			clientID := auth.ClientIDFromContext(ctx)

			ctx, span := tracer.Start(ctx, method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("mcp.method", method),
					attribute.String("mcp.tool.name", toolName),
					attribute.String("mcp.request.id", reqID),
					attribute.String("mcp.client.id", clientID),
				),
			)
			defer span.End()

			attrs := metric.WithAttributes(
				attribute.String("mcp.method", method),
				attribute.String("mcp.tool.name", toolName),
			)

			metrics.ActiveRequests.Add(ctx, 1, attrs)
			start := time.Now()

			result, err := next(ctx, method, req)

			elapsed := time.Since(start)
			metrics.ActiveRequests.Add(ctx, -1, attrs)
			metrics.ToolCallDuration.Record(ctx, elapsed.Seconds(), attrs)

			status := "ok"
			if err != nil {
				status = "error"
				span.RecordError(err)
				span.SetStatus(codes.Error, "tool call failed")
				metrics.ToolErrorCount.Add(ctx, 1, attrs)
			}

			metrics.ToolCallCount.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("mcp.method", method),
					attribute.String("mcp.tool.name", toolName),
					attribute.String("status", status),
				),
			)

			logger.LogAttrs(ctx, slog.LevelInfo, "tool call",
				slog.String("method", method),
				slog.String("tool", toolName),
				slog.String("request_id", reqID),
				slog.String("client_id", clientID),
				slog.Duration("duration", elapsed),
				slog.String("status", status),
			)

			return result, apperrors.SafeForClient(err)
		}
	}
}

// extractToolName attempts to pull the tool name from the request params.
// For tools/call, the concrete params type is *CallToolParamsRaw which has a Name field.
func extractToolName(method string, req mcp.Request) string {
	if method != "tools/call" {
		return method
	}
	type hasName interface {
		GetName() string
	}
	if p, ok := req.GetParams().(hasName); ok {
		return p.GetName()
	}
	if sr, ok := req.(*mcp.ServerRequest[*mcp.CallToolParamsRaw]); ok {
		return sr.Params.Name
	}
	return "unknown"
}
