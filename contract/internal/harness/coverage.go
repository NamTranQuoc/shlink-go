package harness

import (
	"sort"
	"sync"
)

var (
	callsMu sync.Mutex
	calls   = map[string]bool{}
)

func recordCall(method, path string) {
	callsMu.Lock()
	defer callsMu.Unlock()
	calls[method+" "+path] = true
}

// RecordedCalls returns every "METHOD /path" the suite sent to the target so far.
func RecordedCalls() []string {
	callsMu.Lock()
	defer callsMu.Unlock()
	out := make([]string, 0, len(calls))
	for c := range calls {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
