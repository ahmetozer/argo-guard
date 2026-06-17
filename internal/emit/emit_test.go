package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ahmetozer/argo-guard/internal/evaluate"
)

func TestSuccessWritesManifests(t *testing.T) {
	var out bytes.Buffer
	if err := Success(&out, []byte("kind: Service\n")); err != nil {
		t.Fatal(err)
	}
	if out.String() != "kind: Service\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestReportListsDeniesAndWarns(t *testing.T) {
	var errb bytes.Buffer
	res := evaluate.Result{
		Denies: []evaluate.Finding{{Msg: "LoadBalancer not allowed", File: "svc.yaml"}},
		Warns:  []evaluate.Finding{{Msg: "missing owner label", File: "deploy.yaml"}},
	}
	Report(&errb, res, true)
	s := errb.String()
	for _, want := range []string{"LoadBalancer not allowed", "missing owner label", "DENY", "WARN", "stale"} {
		if !strings.Contains(s, want) {
			t.Fatalf("report missing %q:\n%s", want, s)
		}
	}
}
