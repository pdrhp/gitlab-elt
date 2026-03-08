package main

import (
	"io"
	"log/slog"
	"testing"
)

func TestCronLoggerPrintfDoesNotPanic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cl := newCronLogger(logger)

	// Ensure Printf formats entries without panicking; ignore output destination.
	cl.Printf("job finished: %s", "sync")
}
