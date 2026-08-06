// Package argocd resolves deployment history from the Argo CD Application API.
package argocd

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	serviceAccountToken = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	serviceAccountCA    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// Client reads only the Application status required for transition policies.
type Client struct {
	baseURL    string
	namespace  string
	token      string
	httpClient *http.Client
}

// New returns a client with explicitly supplied dependencies. It is primarily
// useful for tests; production callers should use NewInCluster.
func New(baseURL, namespace, token string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		namespace:  namespace,
		token:      token,
		httpClient: httpClient,
	}
}

// NewInCluster creates a client from the repo-server pod's service account.
func NewInCluster(namespace string) (*Client, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, fmt.Errorf("KUBERNETES_SERVICE_HOST/PORT are required")
	}
	if namespace == "" {
		namespace = "argocd"
	}

	token, err := os.ReadFile(serviceAccountToken)
	if err != nil {
		return nil, fmt.Errorf("read service account token: %w", err)
	}
	ca, err := os.ReadFile(serviceAccountCA)
	if err != nil {
		return nil, fmt.Errorf("read service account CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, fmt.Errorf("parse service account CA")
	}

	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	httpClient := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	return New("https://"+net.JoinHostPort(host, port), namespace, strings.TrimSpace(string(token)), httpClient), nil
}

type application struct {
	Spec struct {
		RevisionHistoryLimit *int64      `json:"revisionHistoryLimit"`
		Destination          Destination `json:"destination"`
	} `json:"spec"`
	Status struct {
		History []struct {
			Revision  string   `json:"revision"`
			Revisions []string `json:"revisions"`
			Source    struct {
				RepoURL string `json:"repoURL"`
				Path    string `json:"path"`
			} `json:"source"`
		} `json:"history"`
	} `json:"status"`
}

// Destination is the target recorded on the trusted Argo CD Application.
// Either Server or Name identifies the cluster; Namespace is the requested
// destination namespace.
type Destination struct {
	Server    string `json:"server"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type SourceRevision struct {
	RepoURL     string
	Revision    string
	Path        string
	Destination Destination
}

// LastSuccessfulSource returns the newest successfully deployed Git source.
// Argo CD stores status.history oldest-first and newest-last.
// found=false means the Application has never completed a sync.
func (c *Client) LastSuccessfulSource(appName string) (source SourceRevision, found bool, err error) {
	if appName == "" {
		return SourceRevision{}, false, fmt.Errorf("application name is required")
	}
	path := fmt.Sprintf("/apis/argoproj.io/v1alpha1/namespaces/%s/applications/%s",
		url.PathEscape(c.namespace), url.PathEscape(appName))
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return SourceRevision{}, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return SourceRevision{}, false, fmt.Errorf("get Argo CD Application %s/%s: %w", c.namespace, appName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return SourceRevision{}, false, fmt.Errorf("get Argo CD Application %s/%s: HTTP %d: %s", c.namespace, appName, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var app application
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return SourceRevision{}, false, fmt.Errorf("decode Argo CD Application %s/%s: %w", c.namespace, appName, err)
	}
	if app.Spec.RevisionHistoryLimit != nil && *app.Spec.RevisionHistoryLimit == 0 {
		return SourceRevision{}, false, fmt.Errorf("Application %s/%s has revisionHistoryLimit=0; transition policies require sync history", c.namespace, appName)
	}
	if len(app.Status.History) == 0 {
		return SourceRevision{Destination: app.Spec.Destination}, false, nil
	}
	latest := app.Status.History[len(app.Status.History)-1]
	if latest.Revision == "" {
		if len(latest.Revisions) > 0 {
			return SourceRevision{}, false, fmt.Errorf("Application %s/%s uses multiple sources; transition policies currently support one Git source", c.namespace, appName)
		}
		return SourceRevision{}, false, fmt.Errorf("Application %s/%s latest sync history has no revision", c.namespace, appName)
	}
	return SourceRevision{
		RepoURL:     latest.Source.RepoURL,
		Revision:    latest.Revision,
		Path:        latest.Source.Path,
		Destination: app.Spec.Destination,
	}, true, nil
}
