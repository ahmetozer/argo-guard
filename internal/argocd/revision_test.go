package argocd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func applicationServer(t *testing.T, response string, status int) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/argoproj.io/v1alpha1/namespaces/argocd/applications/payments" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("authorization=%q", got)
		}
		w.WriteHeader(status)
		fmt.Fprint(w, response)
	}))
	return New(server.URL, "argocd", "token", server.Client()), server
}

func TestLastSuccessfulRevisionUsesNewestHistoryEntry(t *testing.T) {
	client, server := applicationServer(t, `{
		"status":{"history":[
			{"id":1,"revision":"aaaa","source":{"repoURL":"https://git.example/old.git","path":"old"}},
			{"id":2,"revision":"bbbb","source":{"repoURL":"https://git.example/app.git","path":"deploy/prod"}}
		]}
	}`, http.StatusOK)
	defer server.Close()
	source, found, err := client.LastSuccessfulSource("payments")
	if err != nil || !found || source.Revision != "bbbb" || source.RepoURL != "https://git.example/app.git" || source.Path != "deploy/prod" {
		t.Fatalf("source=%+v found=%v err=%v", source, found, err)
	}
}

func TestLastSuccessfulRevisionNoHistoryIsNewApp(t *testing.T) {
	client, server := applicationServer(t, `{"status":{}}`, http.StatusOK)
	defer server.Close()
	source, found, err := client.LastSuccessfulSource("payments")
	if err != nil || found || source.Revision != "" {
		t.Fatalf("source=%+v found=%v err=%v", source, found, err)
	}
}

func TestLastSuccessfulRevisionRejectsDisabledHistory(t *testing.T) {
	client, server := applicationServer(t, `{"spec":{"revisionHistoryLimit":0},"status":{}}`, http.StatusOK)
	defer server.Close()
	_, _, err := client.LastSuccessfulSource("payments")
	if err == nil || !strings.Contains(err.Error(), "revisionHistoryLimit=0") {
		t.Fatalf("err=%v", err)
	}
}

func TestLastSuccessfulRevisionRejectsMultipleSources(t *testing.T) {
	client, server := applicationServer(t, `{"status":{"history":[{"revisions":["aaaa","bbbb"]}]}}`, http.StatusOK)
	defer server.Close()
	_, _, err := client.LastSuccessfulSource("payments")
	if err == nil || !strings.Contains(err.Error(), "multiple sources") {
		t.Fatalf("err=%v", err)
	}
}

func TestLastSuccessfulRevisionHTTPFailure(t *testing.T) {
	client, server := applicationServer(t, `forbidden`, http.StatusForbidden)
	defer server.Close()
	_, _, err := client.LastSuccessfulSource("payments")
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("err=%v", err)
	}
}
