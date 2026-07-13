package handler

import "testing"

func TestDetectDelimiter(t *testing.T) {
	cases := []struct {
		name   string
		sample string
		want   rune
	}{
		{"semicolon header", "NAME;DATE_CREATION;PROFIL\n", ';'},
		{"comma header", "NAME,DATE_CREATION,PROFIL\n", ','},
		{"comma with quoted field", `NAME,"ADDRESS, CITY",PROFIL` + "\n", ','},
		{"single column, no delimiter", "NAME\n", ';'}, // default when ambiguous
		{"empty sample", "", ';'},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectDelimiter([]byte(tc.sample)); got != tc.want {
				t.Errorf("detectDelimiter(%q) = %q, want %q", tc.sample, got, tc.want)
			}
		})
	}
}
