// Command report renders the benchmark tables from the raw results of one run (bench/results/<run-id>/) and writes
// them into docs/benchmark.md between the bench markers. Numbers are never edited by hand: rerun this instead.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var scenarios = []string{"redirect", "redirect-no-tracking", "create", "list", "short-url-visits", "tags-stats",
	"global-visits", "mixed"}

var targets = []string{"php", "go"}

var targetNames = map[string]string{"php": "Shlink 5.1.8 (PHP, RoadRunner)", "go": "shlink-go"}

type k6Summary struct {
	Metrics map[string]map[string]float64 `json:"metrics"`
}

func readJSON(path string, v any) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	n := len(values)
	if n%2 == 1 {
		return values[n/2]
	}
	return (values[n/2-1] + values[n/2]) / 2
}

// stats returns average and peak CPU % and memory bytes of a sampler CSV.
func stats(path string) (cpuAvg, cpuPeak, memAvg, memPeak float64) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	rows, _ := csv.NewReader(f).ReadAll()
	n := 0.0
	for _, r := range rows[min(1, len(rows)):] {
		if len(r) < 3 {
			continue
		}
		cpu, _ := strconv.ParseFloat(r[1], 64)
		mem, _ := strconv.ParseFloat(r[2], 64)
		cpuAvg += cpu
		memAvg += mem
		cpuPeak = max(cpuPeak, cpu)
		memPeak = max(memPeak, mem)
		n++
	}
	if n > 0 {
		cpuAvg /= n
		memAvg /= n
	}
	return
}

var memUnit = regexp.MustCompile(`^([\d.]+)([KMG]i?B)$`)

func parseMem(s string) float64 {
	m := memUnit.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	switch m[2][0] {
	case 'K':
		return v * 1024
	case 'M':
		return v * 1024 * 1024
	case 'G':
		return v * 1024 * 1024 * 1024
	}
	return v
}

func mib(b float64) string { return fmt.Sprintf("%.0f MiB", b/1024/1024) }

type row struct {
	rps, p50, p95, p99, errors, cpuAvg, cpuPeak, memAvg, memPeak float64
	capacity                                                     int
	ok                                                           bool
}

func collect(dir, target, scenario string) row {
	var rps, p50, p95, p99, errs, cpuA, cpuP, memA, memP []float64
	for run := 1; run <= 9; run++ {
		tag := fmt.Sprintf("%s.%s.%d", target, scenario, run)
		var s k6Summary
		if !readJSON(filepath.Join(dir, tag+".k6.json"), &s) {
			continue
		}
		d := s.Metrics["http_req_duration"]
		c := s.Metrics["checks"]
		rps = append(rps, s.Metrics["http_reqs"]["rate"])
		p50 = append(p50, d["med"])
		p95 = append(p95, d["p(95)"])
		p99 = append(p99, d["p(99)"])
		errs = append(errs, 100*c["fails"]/max(1, c["passes"]+c["fails"]))
		a, b, m, n := stats(filepath.Join(dir, tag+".stats.csv"))
		cpuA, cpuP, memA, memP = append(cpuA, a), append(cpuP, b), append(memA, m), append(memP, n)
	}
	if len(rps) == 0 {
		return row{}
	}
	r := row{ok: true, rps: median(rps), p50: median(p50), p95: median(p95), p99: median(p99), errors: median(errs),
		cpuAvg: median(cpuA), cpuPeak: median(cpuP), memAvg: median(memA), memPeak: median(memP)}
	files, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("%s.%s.1.capacity-*.json", target, scenario)))
	for _, f := range files {
		rate, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f),
			fmt.Sprintf("%s.%s.1.capacity-", target, scenario)), ".json"))
		var s k6Summary
		if !readJSON(f, &s) {
			continue
		}
		c := s.Metrics["checks"]
		failRate := c["fails"] / max(1, c["passes"]+c["fails"])
		if s.Metrics["http_req_duration"]["p(99)"] <= 500 && failRate <= 0.01 &&
			s.Metrics["http_reqs"]["rate"] >= 0.95*float64(rate) && rate > r.capacity {
			r.capacity = rate
		}
	}
	return r
}

func render(dir string) string {
	var env map[string]any
	readJSON(filepath.Join(dir, "env.json"), &env)
	var b strings.Builder
	fmt.Fprintf(&b, "Run `%v` on %v; Docker %v; app limits %v; k6 `%v`; warm-up %v, measured %v, median of %v runs.\n\n",
		env["runId"], env["host"], env["docker"], env["limits"], env["k6"], env["warmup"], env["duration"], env["runs"])

	b.WriteString("### Latency at a fixed arrival rate\n\n")
	b.WriteString("| Scenario | Target | RPS | p50 (ms) | p95 (ms) | p99 (ms) | Errors | CPU avg / peak | Memory avg / peak |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, s := range scenarios {
		for _, t := range targets {
			r := collect(dir, t, s)
			if !r.ok {
				continue
			}
			fmt.Fprintf(&b, "| %s | %s | %.0f | %.1f | %.1f | %.1f | %.2f%% | %.0f%% / %.0f%% | %s / %s |\n", s,
				targetNames[t], r.rps, r.p50, r.p95, r.p99, r.errors, r.cpuAvg, r.cpuPeak, mib(r.memAvg), mib(r.memPeak))
		}
	}

	b.WriteString("\n### Highest sustained arrival rate (p99 ≤ 500 ms, errors ≤ 1%)\n\n")
	b.WriteString("| Scenario | " + targetNames["php"] + " | " + targetNames["go"] + " | Ratio |\n|---|---:|---:|---:|\n")
	for _, s := range scenarios {
		php, golang := collect(dir, "php", s), collect(dir, "go", s)
		if !php.ok && !golang.ok {
			continue
		}
		ratio := "-"
		if php.capacity > 0 {
			ratio = fmt.Sprintf("%.1fx", float64(golang.capacity)/float64(php.capacity))
		}
		fmt.Fprintf(&b, "| %s | %d req/s | %d req/s | %s |\n", s, php.capacity, golang.capacity, ratio)
	}

	b.WriteString("\n### Footprint\n\n| Metric | " + targetNames["php"] + " | " + targetNames["go"] + " |\n|---|---:|---:|\n")
	sizes, _ := env["imageSizes"].(map[string]any)
	startup := map[string][]float64{}
	idle := map[string][]float64{}
	for _, t := range targets {
		files, _ := filepath.Glob(filepath.Join(dir, t+".*.startup.json"))
		for _, f := range files {
			var s struct {
				StartupMs  float64 `json:"startupMs"`
				IdleMemory string  `json:"idleMemory"`
			}
			if readJSON(f, &s) {
				startup[t] = append(startup[t], s.StartupMs)
				idle[t] = append(idle[t], parseMem(s.IdleMemory))
			}
		}
	}
	size := func(t string) string {
		v, _ := sizes[t].(float64)
		return fmt.Sprintf("%.0f MB", v/1e6)
	}
	fmt.Fprintf(&b, "| Image size | %s | %s |\n", size("php"), size("go"))
	fmt.Fprintf(&b, "| Start to healthy (median) | %.0f ms | %.0f ms |\n", median(startup["php"]), median(startup["go"]))
	fmt.Fprintf(&b, "| Idle memory (median) | %s | %s |\n", mib(median(idle["php"])), mib(median(idle["go"])))

	// Runs whose target was not running after the measurement (crash, OOM kill) are listed so no median hides them
	var crashed []string
	files, _ := filepath.Glob(filepath.Join(dir, "*.state.json"))
	sort.Strings(files)
	for _, f := range files {
		var s struct {
			Running   bool `json:"running"`
			OOMKilled bool `json:"oomKilled"`
			ExitCode  int  `json:"exitCode"`
		}
		if readJSON(f, &s) && (!s.Running || s.OOMKilled) {
			crashed = append(crashed, fmt.Sprintf("`%s` (exit %d, OOM killed: %t)", strings.TrimSuffix(filepath.Base(f),
				".state.json"), s.ExitCode, s.OOMKilled))
		}
	}
	if len(crashed) > 0 {
		b.WriteString("\nThe target was not running at the end of these measurements: " + strings.Join(crashed, ", ") + ".\n")
	}
	return b.String()
}

func main() {
	dir := flag.String("dir", "", "results directory (default: latest under bench/results)")
	doc := flag.String("doc", "docs/benchmark.md", "document to update; empty to only print")
	flag.Parse()
	if *dir == "" {
		dirs, _ := filepath.Glob("bench/results/*")
		sort.Strings(dirs)
		if len(dirs) == 0 {
			log.Fatal("no results")
		}
		*dir = dirs[len(dirs)-1]
	}
	out := render(*dir)
	fmt.Print(out)
	if *doc == "" {
		return
	}
	raw, err := os.ReadFile(*doc)
	if err != nil {
		log.Fatal(err)
	}
	const start, end = "<!-- bench:start -->", "<!-- bench:end -->"
	s := string(raw)
	i, j := strings.Index(s, start), strings.Index(s, end)
	if i < 0 || j < i {
		log.Fatalf("%s has no %s ... %s block", *doc, start, end)
	}
	s = s[:i+len(start)] + "\n" + out + s[j:]
	if err := os.WriteFile(*doc, []byte(s), 0o644); err != nil {
		log.Fatal(err)
	}
}
