package lower

import "testing"

func TestCanonicalStringRaw(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain", value: "MiniGo 世界", want: `"MiniGo 世界"`},
		{name: "json escapes", value: "\x00\n\"\\<>&", want: `"\u0000\n\"\\\u003c\u003e\u0026"`},
		{name: "invalid utf8", value: string([]byte{'a', 0xff, 'b'}), want: `"a\ufffdb"`},
		{name: "replacement rune", value: "a�b", want: `"a\ufffdb"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := string(canonicalStringRaw(test.value)); got != test.want {
				t.Fatalf("canonicalStringRaw(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}
