package telegram

import (
	"reflect"
	"testing"
)

func TestParseLogins(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"spaces", "/face-scripts ivan petr olga", []string{"ivan", "petr", "olga"}},
		{"commas_and_newlines", "/face-scripts ivan,petr\nolga", []string{"ivan", "petr", "olga"}},
		{"bot_suffix", "/face-scripts@TadminBot ivan", []string{"ivan"}},
		{"dedup_preserves_order", "/face-scripts ivan petr ivan", []string{"ivan", "petr"}},
		{"no_logins", "/face-scripts", nil},
		{"only_separators", "/face-scripts , ; \n", nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := parseLogins(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseLogins(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
		})
	}
}
