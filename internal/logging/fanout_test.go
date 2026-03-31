package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestFanoutHandler_BothHandlersReceive(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	h1 := slog.NewTextHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelDebug})
	h2 := slog.NewTextHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelDebug})

	logger := slog.New(fanoutHandler{primary: h1, secondary: h2})
	logger.Info("hello", slog.String("k", "v"))

	if !strings.Contains(buf1.String(), "hello") {
		t.Errorf("primary handler missing log: %s", buf1.String())
	}
	if !strings.Contains(buf2.String(), "hello") {
		t.Errorf("secondary handler missing log: %s", buf2.String())
	}
}

func TestFanoutHandler_Enabled(t *testing.T) {
	debugHandler := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelDebug})
	errorHandler := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError})

	h := fanoutHandler{primary: debugHandler, secondary: errorHandler}

	if !h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("should be enabled at debug (primary accepts)")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("should be enabled at error (both accept)")
	}
}

func TestFanoutHandler_WithAttrs(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	h1 := slog.NewTextHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelDebug})
	h2 := slog.NewTextHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelDebug})

	h := fanoutHandler{primary: h1, secondary: h2}
	withAttrs := h.WithAttrs([]slog.Attr{slog.String("env", "test")})

	logger := slog.New(withAttrs)
	logger.Info("msg")

	if !strings.Contains(buf1.String(), "env=test") {
		t.Errorf("primary missing attr: %s", buf1.String())
	}
	if !strings.Contains(buf2.String(), "env=test") {
		t.Errorf("secondary missing attr: %s", buf2.String())
	}
}

func TestFanoutHandler_WithGroup(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	h1 := slog.NewTextHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelDebug})
	h2 := slog.NewTextHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelDebug})

	h := fanoutHandler{primary: h1, secondary: h2}
	withGroup := h.WithGroup("grp")

	logger := slog.New(withGroup)
	logger.Info("msg", slog.String("k", "v"))

	if !strings.Contains(buf1.String(), "grp.k=v") {
		t.Errorf("primary missing group prefix: %s", buf1.String())
	}
	if !strings.Contains(buf2.String(), "grp.k=v") {
		t.Errorf("secondary missing group prefix: %s", buf2.String())
	}
}
