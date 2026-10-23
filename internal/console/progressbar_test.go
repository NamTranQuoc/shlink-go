package console

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestProgressBarMatchesSymfony replays the calls of testdata/progressbar.php, whose output was captured from the
// upstream image (docker run --rm -e COLUMNS=120 -v "$PWD/internal/console/testdata:/pb:ro" --entrypoint php
// shlinkio/shlink:5.1.8 /pb/progressbar.php). The clock does not move, like PHP running the calls back to back.
func TestProgressBarMatchesSymfony(t *testing.T) {
	t.Setenv("COLUMNS", "120")
	raw, err := os.ReadFile("testdata/progressbar.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]string
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	type call struct {
		op string
		n  int
	}
	cases := map[string]struct {
		decorated bool
		verbosity int
		calls     []call
	}{
		"jump-to-max":    {false, VerbosityNormal, []call{{"max", 1000}, {"progress", 1000}, {"finish", 0}}},
		"rapid-steps":    {false, VerbosityNormal, []call{{"max", 300}, {"progress", 0}, {"progress", 100}, {"progress", 200}, {"progress", 300}, {"finish", 0}}},
		"first-then-max": {false, VerbosityNormal, []call{{"max", 52428800}, {"progress", 16384}, {"progress", 52428800}, {"finish", 0}}},
		"unknown-size":   {false, VerbosityNormal, []call{{"max", 0}, {"progress", 0}, {"progress", 4096}, {"finish", 0}}},
		"over-max":       {false, VerbosityNormal, []call{{"max", 10}, {"progress", 15}, {"finish", 0}}},
		"decorated":      {true, VerbosityNormal, []call{{"max", 300}, {"progress", 0}, {"progress", 300}, {"finish", 0}}},
		"verbose":        {false, VerbosityVerbose, []call{{"max", 300}, {"progress", 300}, {"finish", 0}}},
		"very-verbose":   {false, VerbosityVeryVerbose, []call{{"max", 300}, {"progress", 300}, {"finish", 0}}},
		"quiet":          {false, VerbosityQuiet, []call{{"max", 300}, {"progress", 300}, {"finish", 0}}},
	}
	if len(cases) != len(golden) {
		t.Fatalf("%d cases, %d goldens", len(cases), len(golden))
	}
	now := time.Unix(1_800_000_000, 0)
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			out := NewStreamOutput(&buf)
			out.SetDecorated(c.decorated)
			out.SetVerbosity(c.verbosity)
			io := NewSymfonyStyle(NewArrayInput(map[string]any{}), out, nil)
			io.Text("<fg=blue>Downloading GeoLite2 db file...</>")
			bar := newProgressBar(io, func() time.Time { return now })
			for _, call := range c.calls {
				switch call.op {
				case "max":
					bar.SetMaxSteps(call.n)
				case "progress":
					bar.SetProgress(call.n)
				case "finish":
					bar.Finish()
				}
			}
			io.Success("GeoLite2 db file properly downloaded.")
			if got := buf.String(); got != golden[name] {
				t.Errorf("got  %q\nwant %q", got, golden[name])
			}
		})
	}
}

// TestFormatTime pins Helper::formatTime($secs, 2) as the upstream image prints it.
func TestFormatTime(t *testing.T) {
	for secs, want := range map[float64]string{0: "< 1 ms", 0.5: "500 ms", 1: "1 s", 61: "1 min, 1 s", 3725: "1 h, 2 min", 90061: "1 d, 1 h"} {
		if got := FormatTime(secs, 2); got != want {
			t.Errorf("FormatTime(%v) = %q, want %q", secs, got, want)
		}
	}
}
