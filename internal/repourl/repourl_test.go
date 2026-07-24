package repourl

import "testing"

func TestEqualNormalizesSafeEquivalentForms(t *testing.T) {
	tests := [][2]string{
		{"https://GIT.EXAMPLE/team/app.git", "https://git.example:443/team/app/"},
		{"git@git.example:team/app.git", "ssh://git@git.example:22/team/app"},
	}
	for _, test := range tests {
		equal, err := Equal(test[0], test[1])
		if err != nil || !equal {
			t.Fatalf("Equal(%q, %q)=%v, %v", test[0], test[1], equal, err)
		}
	}
}

func TestEqualRejectsDifferentCredentialScopes(t *testing.T) {
	tests := [][2]string{
		{"https://git.example/team/app.git", "https://git.example/team/other.git"},
		{"https://git.example/team/app.git", "https://other.example/team/app.git"},
		{"https://git.example/team/app.git", "ssh://git@git.example/team/app.git"},
		{"ssh://git@git.example/team/app.git", "ssh://deploy@git.example/team/app.git"},
	}
	for _, test := range tests {
		equal, err := Equal(test[0], test[1])
		if err != nil {
			t.Fatalf("Equal(%q, %q): %v", test[0], test[1], err)
		}
		if equal {
			t.Fatalf("repositories must differ: %q and %q", test[0], test[1])
		}
	}
}

func TestEqualRejectsSecretsAndMalformedURLsWithoutEchoingThem(t *testing.T) {
	secret := "do-not-log-this"
	for _, raw := range []string{"", "not-a-url", "https://user:" + secret + "@git.example/team/app.git"} {
		_, err := Equal("https://git.example/team/app.git", raw)
		if err == nil {
			t.Fatalf("expected %q to fail", raw)
		}
		if contains(err.Error(), secret) {
			t.Fatalf("error leaked secret: %v", err)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
