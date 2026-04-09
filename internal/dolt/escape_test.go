package dolt

import "testing"

func TestEscapeLIKE(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"%", `\%`},
		{"_", `\_`},
		{"100%_done", `100\%\_done`},
		{"no special chars", "no special chars"},
		{"%_%", `\%\_\%`},
		{"", ""},
	}
	for _, tt := range tests {
		got := escapeLIKE(tt.input)
		if got != tt.want {
			t.Errorf("escapeLIKE(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
