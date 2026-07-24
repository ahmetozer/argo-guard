// Package repourl compares Git repository identities without exposing URLs or
// embedded credentials in errors.
package repourl

import (
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// Equal reports whether two URLs identify the same repository and transport.
// It is deliberately conservative: HTTPS and SSH are not interchangeable for
// credential forwarding.
func Equal(left, right string) (bool, error) {
	a, err := normalize(left)
	if err != nil {
		return false, fmt.Errorf("normalize current repository URL: %w", err)
	}
	b, err := normalize(right)
	if err != nil {
		return false, fmt.Errorf("normalize previous repository URL: %w", err)
	}
	return a == b, nil
}

func normalize(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n\t") {
		return "", fmt.Errorf("repository URL is missing or malformed")
	}
	if !strings.Contains(raw, "://") {
		return normalizeSCP(raw)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "", fmt.Errorf("repository URL is malformed")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("repository URL must not contain a query or fragment")
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			return "", fmt.Errorf("repository URL must not embed a password")
		}
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "file" {
		clean, err := cleanRepoPath(u.Path)
		if err != nil {
			return "", err
		}
		return "file||||" + clean, nil
	}
	if scheme != "http" && scheme != "https" && scheme != "ssh" && scheme != "git" {
		return "", fmt.Errorf("repository URL uses an unsupported scheme")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("repository URL has no host")
	}
	port := u.Port()
	if port == "" {
		switch scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		case "ssh":
			port = "22"
		case "git":
			port = "9418"
		}
	} else {
		numericPort, err := strconv.Atoi(port)
		if err != nil || numericPort < 1 || numericPort > 65535 {
			return "", fmt.Errorf("repository URL has an invalid port")
		}
	}
	user := ""
	if u.User != nil {
		user = u.User.Username()
	}
	clean, err := cleanRepoPath(u.EscapedPath())
	if err != nil {
		return "", err
	}
	return strings.Join([]string{scheme, user, host, port, clean}, "|"), nil
}

func normalizeSCP(raw string) (string, error) {
	colon := strings.IndexByte(raw, ':')
	if colon <= 0 || colon == len(raw)-1 || strings.Contains(raw[:colon], "/") {
		return "", fmt.Errorf("repository URL is malformed")
	}
	authority, repoPath := raw[:colon], raw[colon+1:]
	user, host := "", authority
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		user, host = authority[:at], authority[at+1:]
	}
	if host == "" || strings.ContainsAny(host, "@:") {
		return "", fmt.Errorf("repository URL has an invalid SSH host")
	}
	clean, err := cleanRepoPath(repoPath)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{"ssh", user, strings.ToLower(host), "22", clean}, "|"), nil
}

func cleanRepoPath(raw string) (string, error) {
	decoded, err := url.PathUnescape(raw)
	if err != nil || strings.ContainsAny(decoded, "\r\n\t") {
		return "", fmt.Errorf("repository URL path is malformed")
	}
	clean := strings.TrimPrefix(path.Clean("/"+decoded), "/")
	clean = strings.TrimSuffix(strings.TrimSuffix(clean, "/"), ".git")
	clean = strings.TrimSuffix(clean, "/")
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("repository URL path is missing or malformed")
	}
	return clean, nil
}
