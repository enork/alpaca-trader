package logutil

import (
	"log/slog"
	"os"
	"time"
)

var central = func() *time.Location {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		panic("logutil: failed to load America/Chicago timezone: " + err.Error())
	}
	return loc
}()

// New returns a JSON slog.Logger that writes to stdout with timestamps in
// Central Time (America/Chicago).
func New(opts *slog.HandlerOptions) *slog.Logger {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	prev := opts.ReplaceAttr
	opts.ReplaceAttr = func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && a.Key == slog.TimeKey {
			a.Value = slog.TimeValue(a.Value.Time().In(central))
		}
		if prev != nil {
			return prev(groups, a)
		}
		return a
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
