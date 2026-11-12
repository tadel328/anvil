package anvil

import (
	"strings"
	"testing"
)

func TestParseDotenv(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		want    map[string]string
		wantErr string
	}{
		{
			name: "empty file",
			give: "",
			want: map[string]string{},
		},
		{
			name: "simple pairs",
			give: "A=1\nB=two\nC=three",
			want: map[string]string{"A": "1", "B": "two", "C": "three"},
		},
		{
			name: "comments and blank lines",
			give: "# top\nA=1\n\n  # indented\nB=2\n",
			want: map[string]string{"A": "1", "B": "2"},
		},
		{
			name: "export prefix",
			give: "export A=1\nexport B=2",
			want: map[string]string{"A": "1", "B": "2"},
		},
		{
			name: "single quotes are literal",
			give: `A='a #b'`,
			want: map[string]string{"A": "a #b"},
		},
		{
			name: "double quotes with escapes",
			give: `A="a\nb\tc\"d\\e"`,
			want: map[string]string{"A": "a\nb\tc\"d\\e"},
		},
		{
			name: "unquoted inline comment",
			give: "A=value # comment",
			want: map[string]string{"A": "value"},
		},
		{
			name: "unquoted hash without space is value",
			give: "A=value#not-comment",
			want: map[string]string{"A": "value#not-comment"},
		},
		{
			name:    "unterminated single quote",
			give:    "A='oops",
			wantErr: "unterminated single quote",
		},
		{
			name:    "unterminated double quote",
			give:    `A="oops`,
			wantErr: "unterminated double quote",
		},
		{
			name:    "text after closing quote",
			give:    `A="v" junk`,
			wantErr: "unexpected text after the closing quote",
		},
		{
			name:    "line without equals",
			give:    "A",
			wantErr: "want KEY=VALUE",
		},
		{
			name:    "key with space",
			give:    "A B=1",
			wantErr: "want KEY=VALUE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDotenv(tt.give)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parseDotenv(%q) expected error %q, got nil", tt.give, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseDotenv(%q) error = %q, want containing %q", tt.give, err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseDotenv(%q) unexpected error: %v", tt.give, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseDotenv(%q) = %v, want %v", tt.give, got, tt.want)
			}
			for k, want := range tt.want {
				if got[k] != want {
					t.Errorf("parseDotenv(%q)[%q] = %q, want %q", tt.give, k, got[k], want)
				}
			}
		})
	}
}

func TestEnvQuoteRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		give string
	}{
		{name: "plain", give: "hello"},
		{name: "spaces", give: "hello world"},
		{name: "hash", give: "a #b"},
		{name: "single quote", give: "it's"},
		{name: "double quote", give: `say "hi"`},
		{name: "dollar", give: "$HOME"},
		{name: "backslash", give: `C:\path`},
		{name: "newline", give: "line1\nline2"},
		{name: "carriage return", give: "a\rb"},
		{name: "combination", give: "a 'b' \"c\"\n#d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quoted := envQuote(tt.give)

			parsed, err := parseDotenv("K=" + quoted)
			if err != nil {
				t.Fatalf("parseDotenv of envQuote(%q)=%q failed: %v", tt.give, quoted, err)
			}
			if parsed["K"] != tt.give {
				t.Errorf("round-trip envQuote(%q) = %q, parsed back %q", tt.give, quoted, parsed["K"])
			}
		})
	}
}
