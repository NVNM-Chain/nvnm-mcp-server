package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Config holds telemetry configuration loaded from environment variables.
type Config struct {
	ServiceName      string
	ServiceVersion   string
	OTLPEndpoint     string
	EnablePrometheus bool
	EnableStdout     bool
}

// Telemetry manages the OpenTelemetry TracerProvider, MeterProvider,
// and Prometheus scrape handler.
type Telemetry struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	promHandler    http.Handler
	Metrics        *Metrics
	logger         *slog.Logger
}

// New initializes OpenTelemetry providers and exporters.
func New(ctx context.Context, cfg Config, logger *slog.Logger) (*Telemetry, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create otel resource: %w", err)
	}

	tp, err := buildTracerProvider(ctx, cfg, res, logger)
	if err != nil {
		return nil, fmt.Errorf("create tracer provider: %w", err)
	}
	otel.SetTracerProvider(tp)

	mp, promHandler, err := buildMeterProvider(ctx, cfg, res, logger)
	if err != nil {
		//nolint:errcheck,gosec // best-effort cleanup on init failure
		tp.Shutdown(ctx)
		return nil, fmt.Errorf("create meter provider: %w", err)
	}
	otel.SetMeterProvider(mp)

	metrics, err := NewMetrics(mp)
	if err != nil {
		//nolint:errcheck,gosec // best-effort cleanup on init failure
		tp.Shutdown(ctx)
		//nolint:errcheck,gosec // best-effort cleanup on init failure
		mp.Shutdown(ctx)
		return nil, fmt.Errorf("create metrics: %w", err)
	}

	logger.Info("telemetry initialized",
		slog.String("service", cfg.ServiceName),
		slog.Bool("otlp", cfg.OTLPEndpoint != ""),
		slog.Bool("prometheus", cfg.EnablePrometheus),
		slog.Bool("stdout", cfg.EnableStdout),
	)

	return &Telemetry{
		tracerProvider: tp,
		meterProvider:  mp,
		promHandler:    promHandler,
		Metrics:        metrics,
		logger:         logger,
	}, nil
}

// PrometheusHandler returns the HTTP handler for /metrics, or nil if
// Prometheus is disabled.
func (t *Telemetry) PrometheusHandler() http.Handler {
	return t.promHandler
}

// Shutdown flushes pending telemetry data. Call from main before exit.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	t.logger.Info("flushing telemetry")
	var errs []error
	if err := t.tracerProvider.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("tracer shutdown: %w", err))
	}
	if err := t.meterProvider.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("meter shutdown: %w", err))
	}
	return errors.Join(errs...)
}

func buildTracerProvider(
	ctx context.Context,
	cfg Config,
	res *resource.Resource,
	logger *slog.Logger,
) (*sdktrace.TracerProvider, error) {
	var opts []sdktrace.TracerProviderOption
	opts = append(opts, sdktrace.WithResource(res))

	if cfg.OTLPEndpoint != "" {
		exp, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			return nil, fmt.Errorf("otlp trace exporter: %w", err)
		}
		opts = append(opts, sdktrace.WithBatcher(exp))
		logger.Debug("otlp trace exporter enabled", slog.String("endpoint", cfg.OTLPEndpoint))
	}

	if cfg.EnableStdout {
		exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("stdout trace exporter: %w", err)
		}
		opts = append(opts, sdktrace.WithBatcher(exp))
	}

	return sdktrace.NewTracerProvider(opts...), nil
}

func buildMeterProvider(
	ctx context.Context,
	cfg Config,
	res *resource.Resource,
	logger *slog.Logger,
) (*sdkmetric.MeterProvider, http.Handler, error) {
	var (
		opts        []sdkmetric.Option
		promHandler http.Handler
	)
	opts = append(opts, sdkmetric.WithResource(res))

	if cfg.EnablePrometheus {
		registry := promclient.NewRegistry()
		exp, err := prometheus.New(prometheus.WithRegisterer(registry))
		if err != nil {
			return nil, nil, fmt.Errorf("prometheus exporter: %w", err)
		}
		opts = append(opts, sdkmetric.WithReader(exp))
		promHandler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
		logger.Debug("prometheus metrics exporter enabled")
	}

	if cfg.OTLPEndpoint != "" {
		exp, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlpmetricgrpc.WithInsecure(),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("otlp metric exporter: %w", err)
		}
		opts = append(opts, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(exp),
		))
		logger.Debug("otlp metric exporter enabled", slog.String("endpoint", cfg.OTLPEndpoint))
	}

	if cfg.EnableStdout {
		exp, err := stdoutmetric.New()
		if err != nil {
			return nil, nil, fmt.Errorf("stdout metric exporter: %w", err)
		}
		opts = append(opts, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(exp),
		))
	}

	return sdkmetric.NewMeterProvider(opts...), promHandler, nil
}
