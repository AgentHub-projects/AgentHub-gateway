package log

import (
	"bufio"
	"io"
	"log/slog"
	"os"
)

func NewFlushWriter(w io.Writer) io.Writer {
	bw := bufio.NewWriter(w)
	return &flushWriter{bw: bw}
}

type flushWriter struct {
	bw *bufio.Writer
}

func (fw *flushWriter) Write(p []byte) (n int, err error) {
	n, err = fw.bw.Write(p)
	if err != nil {
		return n, err
	}
	return n, fw.bw.Flush()
}

func NewStandardLogger(level slog.Level) *slog.Logger {
	w := NewFlushWriter(os.Stdout)
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	}))
}

func NewStandardTextLogger(level slog.Level) *slog.Logger {
	w := NewFlushWriter(os.Stdout)
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: level,
	}))
}
