// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

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

const tracerName = "nvnm-mcp-server"

type requestIDKey struct{}

// RequestIDFromContext returns the request ID attached by the middleware, if any.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// NewMCPMiddleware returns MCP receiving middleware that creates an
// OTel span and records metrics for every incoming method call.
//
// Privacy: tool arguments and return values are never recorded. Errors
// are attached to span events for internal debugging only; the error
// returned to the MCP client is passed through apperrors.SafeForClient
// so URLs, hostnames, and other internal details do not leak across
// the trust boundary.
func NewMCPMiddleware(metrics *Metrics, logger *slog.Logger) mcp.Middleware {
	tracer := otel.Tracer(tracerName)

	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			reqID := uuid.New().String()
			ctx = context.WithValue(ctx, requestIDKey{}, reqID)

			toolName := extractToolName(method, req)
			clientID := auth.ClientIDFromContext(ctx)

			spanAttrs := []attribute.KeyValue{
				attribute.String("mcp.method", method),
				attribute.String("mcp.tool.name", toolName),
				attribute.String("mcp.request.id", reqID),
			}
			if clientID != "" {
				spanAttrs = append(spanAttrs, attribute.String("mcp.client.id", clientID))
			}
			ctx, span := tracer.Start(ctx, method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(spanAttrs...),
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

			logAttrs := []slog.Attr{
				slog.String("method", method),
				slog.String("tool", toolName),
				slog.String("request_id", reqID),
				slog.Duration("duration", elapsed),
				slog.String("status", status),
			}
			if clientID != "" {
				logAttrs = append(logAttrs, slog.String("client_id", clientID))
			}
			logger.LogAttrs(ctx, slog.LevelInfo, "tool call", logAttrs...)

			return result, apperrors.SafeForClient(err)
		}
	}
}

// extractToolName attempts to pull the tool name from the request params.
// For tools/call, the concrete params type is *CallToolParamsRaw which has a Name field.
//
// DUPLICATION: the tools/call branch mirrors internal/mcp.ToolNameFromRequest
// verbatim. Keep them in sync. A full DRY refactor is blocked by an import
// cycle (internal/mcp already imports internal/telemetry); see the note on
// the mcp-side function for the trade-off analysis.
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
