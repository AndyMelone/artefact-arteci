package dateparser

import (
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		hint     Hint
		wantOut  string
		wantParsed bool
	}{
		// MDY numeric
		{"MDY slash", "07/17/2019", HintMDY, "17-07-2019 00:00:00", true},
		{"MDY slash with time", "07/17/2019 00:00:00", HintMDY, "17-07-2019 00:00:00", true},
		{"MDY dash", "07-17-2019", HintMDY, "17-07-2019 00:00:00", true},

		// DMY numeric
		{"DMY slash", "17/07/2019", HintDMY, "17-07-2019 00:00:00", true},
		{"DMY slash with time", "17/07/2019 00:00:00", HintDMY, "17-07-2019 00:00:00", true},
		{"DMY dash", "17-07-2019", HintDMY, "17-07-2019 00:00:00", true},

		// ISO
		{"ISO extended", "2019-07-17", HintMDY, "17-07-2019 00:00:00", true},
		{"ISO with time", "2019-07-17T14:30:00", HintMDY, "17-07-2019 14:30:00", true},
		{"ISO with Z", "2019-07-17T14:30:00Z", HintMDY, "17-07-2019 14:30:00", true},
		{"ISO basic", "20190717", HintMDY, "17-07-2019 00:00:00", true},

		// Unix timestamps
		{"unix seconds", "1563321600", HintMDY, "17-07-2019 00:00:00", true},
		{"unix millis", "1563321600000", HintMDY, "17-07-2019 00:00:00", true},

		// English text
		{"English text", "Jul 17, 2019", HintMDY, "17-07-2019 00:00:00", true},
		{"English full", "July 17, 2019", HintMDY, "17-07-2019 00:00:00", true},

		// French text
		{"French text", "17 juillet 2019", HintDMY, "17-07-2019 00:00:00", true},
		{"French abbr", "17 juil 2019", HintDMY, "17-07-2019 00:00:00", true},

		// AM/PM
		{"AM/PM morning", "7/17/2019 8:30:00 AM", HintMDY, "17-07-2019 08:30:00", true},
		{"AM/PM afternoon", "7/17/2019 2:30:00 PM", HintMDY, "17-07-2019 14:30:00", true},

		// Mixed-format fallback: hint=DMY but value is unambiguously MDY (month field > 12 under DMY)
		{"DMY hint MDY value month>12", "12/19/2023", HintDMY, "19-12-2023 00:00:00", true},
		{"DMY hint MDY value month>12 b", "08/27/2023", HintDMY, "27-08-2023 00:00:00", true},
		{"DMY hint MDY leap day", "02/29/2024", HintDMY, "29-02-2024 00:00:00", true},

		// Mixed-format fallback: hint=MDY but value is unambiguously DMY (day field > 12 under MDY)
		{"MDY hint DMY value day>12", "19/12/2023", HintMDY, "19-12-2023 00:00:00", true},
		{"MDY hint DMY value day>12 b", "27/08/2023", HintMDY, "27-08-2023 00:00:00", true},

		// Edge cases
		{"empty string", "", HintMDY, "", false},
		{"whitespace only", "   ", HintMDY, "", false},
		{"invalid date", "99/99/9999", HintMDY, "99/99/9999", false},
		{"already normalized", "17-07-2019 00:00:00", HintDMY, "17-07-2019 00:00:00", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Normalize(tc.input, tc.hint, "")
			if res.WasParsed != tc.wantParsed {
				t.Errorf("WasParsed: got %v, want %v", res.WasParsed, tc.wantParsed)
			}
			if res.WasParsed && res.Normalized != tc.wantOut {
				t.Errorf("Normalized: got %q, want %q", res.Normalized, tc.wantOut)
			}
		})
	}
}

func TestNormalize_LeapYear(t *testing.T) {
	res := Normalize("02/29/2020", HintMDY, "")
	if !res.WasParsed {
		t.Fatal("expected leap day to be parsed")
	}
	if res.Normalized != "29-02-2020 00:00:00" {
		t.Errorf("got %q", res.Normalized)
	}
}

func TestNormalize_InvalidLeapYear(t *testing.T) {
	res := Normalize("02/29/2019", HintMDY, "")
	if res.WasParsed {
		t.Error("Feb 29 in non-leap year should not be parsed")
	}
}
