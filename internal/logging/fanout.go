package logging

import (
	"context"
	"log/slog"
)

// fanoutHandler sends every log record to both a primary handler (JSON/text
// output) and a secondary handler (OTel bridge for trace correlation).
type fanoutHandler struct {
	primary   slog.Handler
	secondary slog.Handler
}

func (h fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.primary.Enabled(ctx, level) || h.secondary.Enabled(ctx, level)
}

//nolint:gocritic // hugeParam: slog.Handler.Handle signature requires slog.Record by value
func (h fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.primary.Enabled(ctx, r.Level) {
		if err := h.primary.Handle(ctx, r); err != nil {
			return err
		}
	}
	if h.secondary.Enabled(ctx, r.Level) {
		//nolint:errcheck,gosec // best-effort: OTel bridge errors are non-fatal
		h.secondary.Handle(ctx, r)
	}
	return nil
}

func (h fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return fanoutHandler{
		primary:   h.primary.WithAttrs(attrs),
		secondary: h.secondary.WithAttrs(attrs),
	}
}

func (h fanoutHandler) WithGroup(name string) slog.Handler {
	return fanoutHandler{
		primary:   h.primary.WithGroup(name),
		secondary: h.secondary.WithGroup(name),
	}
}
