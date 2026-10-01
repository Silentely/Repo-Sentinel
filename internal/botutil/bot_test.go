package botutil

import "testing"

func TestIsBotUser(t *testing.T) {
	cases := []struct {
		name     string
		login    string
		userType string
		want     bool
	}{
		{name: "dependabot with user type", login: "dependabot[bot]", userType: "User", want: true},
		{name: "renovate with bot type", login: "renovate[bot]", userType: "Bot", want: true},
		{name: "github actions suffix only", login: "github-actions[bot]", userType: "", want: true},
		{name: "app with bot type", login: "some-service-app", userType: "Bot", want: true},
		{name: "app with bot type case insensitive", login: "custom-app", userType: "bot", want: true},
		{name: "dependabot whitelist", login: "dependabot", userType: "", want: true},
		{name: "renovate whitelist", login: "renovate", userType: "", want: true},
		{name: "github-actions whitelist", login: "github-actions", userType: "", want: true},
		{name: "greenkeeper whitelist", login: "greenkeeper", userType: "", want: true},
		{name: "snyk-bot whitelist", login: "snyk-bot", userType: "", want: true},
		{name: "codecov whitelist", login: "codecov", userType: "", want: true},
		{name: "copilot whitelist", login: "copilot", userType: "", want: true},
		{name: "normal human alice", login: "alice", userType: "User", want: false},
		{name: "normal human bob empty type", login: "bob", userType: "", want: false},
		{name: "empty login and type", login: "", userType: "", want: false},
		{name: "whitespace login with bot suffix", login: "  dependabot[bot]  ", userType: "", want: true},
		{name: "whitespace bot user type", login: "some-agent", userType: "  Bot  ", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsBotUser(tc.login, tc.userType)
			if got != tc.want {
				t.Errorf("IsBotUser(%q, %q) = %v, want %v", tc.login, tc.userType, got, tc.want)
			}
		})
	}
}
