// Package emit writes rendered manifests to stdout (pass) or a violation
// report to stderr (fail/warn).
package emit

import (
	"fmt"
	"io"

	"github.com/ahmetozer/argo-guard/internal/evaluate"
)

// Success writes the rendered manifests verbatim for Argo to apply.
func Success(stdout io.Writer, rendered []byte) error {
	_, err := stdout.Write(rendered)
	return err
}

// Report writes a readable summary of findings to stderr. stale indicates the
// policy cache could not be refreshed and last-known-good was used.
func Report(stderr io.Writer, res evaluate.Result, stale bool) {
	fmt.Fprintln(stderr, "argo-guard policy report")
	if stale {
		fmt.Fprintln(stderr, "  WARNING: policy cache is stale (last-known-good served; repo unreachable)")
	}
	for _, d := range res.Denies {
		fmt.Fprintf(stderr, "  DENY  [%s] %s\n", d.File, d.Msg)
	}
	for _, w := range res.Warns {
		fmt.Fprintf(stderr, "  WARN  [%s] %s\n", w.File, w.Msg)
	}
	fmt.Fprintf(stderr, "  summary: %d deny, %d warn\n", len(res.Denies), len(res.Warns))
}
