package dateparser

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

const outputLayout = "02-01-2006 15:04:05"

type Hint string

const (
	HintDMY Hint = "DMY"
	HintMDY Hint = "MDY"
)

type Result struct {
	Normalized    string
	WasParsed     bool
	MatchedFormat string
}

var (
	// d/M/yyyy or dd/MM/yyyy or d-M-yyyy or dd-MM-yyyy + optional HH:mm[:ss]
	numericRe = regexp.MustCompile(
		`^(\d{1,2})([-/])(\d{1,2})[-/](\d{2,4})(?:[ T](\d{2}):(\d{2})(?::(\d{2}))?)?$`,
	)
	// ISO extended: yyyy-MM-dd[T ]HH:mm[:ss[.ms]][Z|±hh:mm]
	isoExtRe = regexp.MustCompile(
		`^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?)?(?:Z|[+-]\d{2}:?\d{2})?$`,
	)
	// ISO basic: yyyyMMdd
	isoBasicRe    = regexp.MustCompile(`^(\d{4})(\d{2})(\d{2})$`)
	unixSecondsRe = regexp.MustCompile(`^\d{10}$`)
	unixMillisRe  = regexp.MustCompile(`^\d{13}$`)
	// M/d/yyyy h:mm[:ss] AM/PM
	ampmRe = regexp.MustCompile(
		`(?i)^(\d{1,2})/(\d{1,2})/(\d{4})\s+(\d{1,2}):(\d{2})(?::(\d{2}))?\s*(AM|PM)$`,
	)
)

var frMonths = map[string]int{
	"janvier": 1, "février": 2, "fevrier": 2, "mars": 3,
	"avril": 4, "mai": 5, "juin": 6, "juillet": 7,
	"août": 8, "aout": 8, "septembre": 9, "octobre": 10,
	"novembre": 11, "décembre": 12, "decembre": 12,
	"janv": 1, "févr": 2, "fevr": 2, "avr": 4,
	"juil": 7, "sept": 9, "oct": 10, "nov": 11, "déc": 12, "dec": 12,
}

var enMonths = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
	"january": 1, "february": 2, "march": 3, "april": 4, "june": 6,
	"july": 7, "august": 8, "september": 9, "october": 10, "november": 11, "december": 12,
}

var goStdLayouts = []string{
	"Mon Jan 02 15:04:05 MST 2006",
	"Mon Jan 2 15:04:05 MST 2006",
}

func fmt2(t time.Time) string { return t.UTC().Format(outputLayout) }

func buildDate(y, mo, d, H, min, s int) (time.Time, bool) {
	if y < 100 {
		y += 2000
	}
	t := time.Date(y, time.Month(mo), d, H, min, s, 0, time.UTC)
	if int(t.Month()) != mo || t.Day() != d {
		return time.Time{}, false
	}
	return t, true
}

func tryISO(raw string) (time.Time, string, bool) {
	if m := isoExtRe.FindStringSubmatch(raw); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		H, min, s := 0, 0, 0
		if m[4] != "" {
			H, _ = strconv.Atoi(m[4])
			min, _ = strconv.Atoi(m[5])
		}
		if m[6] != "" {
			s, _ = strconv.Atoi(m[6])
		}
		if t, ok := buildDate(y, mo, d, H, min, s); ok {
			label := "yyyy-MM-dd"
			if m[4] != "" {
				if m[6] != "" {
					label = "yyyy-MM-dd HH:mm:ss"
				} else {
					label = "yyyy-MM-dd HH:mm"
				}
			}
			return t, label, true
		}
	}
	if m := isoBasicRe.FindStringSubmatch(raw); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if t, ok := buildDate(y, mo, d, 0, 0, 0); ok {
			return t, "yyyyMMdd", true
		}
	}
	return time.Time{}, "", false
}

func tryNumericDMY(raw string) (time.Time, string, bool) {
	m := numericRe.FindStringSubmatch(raw)
	if m == nil {
		return time.Time{}, "", false
	}
	d, _ := strconv.Atoi(m[1])
	sep := m[2]
	mo, _ := strconv.Atoi(m[3])
	y, _ := strconv.Atoi(m[4])
	H, min, s := 0, 0, 0
	if m[5] != "" {
		H, _ = strconv.Atoi(m[5])
		min, _ = strconv.Atoi(m[6])
	}
	if m[7] != "" {
		s, _ = strconv.Atoi(m[7])
	}
	if mo < 1 || mo > 12 || d < 1 || d > 31 {
		return time.Time{}, "", false
	}
	t, ok := buildDate(y, mo, d, H, min, s)
	if !ok {
		return time.Time{}, "", false
	}
	fmtD, fmtM, fmtY := "d", "M", "yyyy"
	if len(m[1]) > 1 {
		fmtD = "dd"
	}
	if len(m[3]) > 1 {
		fmtM = "MM"
	}
	if len(m[4]) == 2 {
		fmtY = "yy"
	}
	label := fmtD + sep + fmtM + sep + fmtY
	if m[5] != "" {
		if m[7] != "" {
			label += " HH:mm:ss"
		} else {
			label += " HH:mm"
		}
	}
	return t, label, true
}

func tryNumericMDY(raw string) (time.Time, string, bool) {
	m := numericRe.FindStringSubmatch(raw)
	if m == nil {
		return time.Time{}, "", false
	}
	mo, _ := strconv.Atoi(m[1])
	sep := m[2]
	d, _ := strconv.Atoi(m[3])
	y, _ := strconv.Atoi(m[4])
	H, min, s := 0, 0, 0
	if m[5] != "" {
		H, _ = strconv.Atoi(m[5])
		min, _ = strconv.Atoi(m[6])
	}
	if m[7] != "" {
		s, _ = strconv.Atoi(m[7])
	}
	if mo < 1 || mo > 12 || d < 1 || d > 31 {
		return time.Time{}, "", false
	}
	t, ok := buildDate(y, mo, d, H, min, s)
	if !ok {
		return time.Time{}, "", false
	}
	fmtM, fmtD, fmtY := "M", "d", "yyyy"
	if len(m[1]) > 1 {
		fmtM = "MM"
	}
	if len(m[3]) > 1 {
		fmtD = "dd"
	}
	if len(m[4]) == 2 {
		fmtY = "yy"
	}
	label := fmtM + sep + fmtD + sep + fmtY
	if m[5] != "" {
		if m[7] != "" {
			label += " HH:mm:ss"
		} else {
			label += " HH:mm"
		}
	}
	return t, label, true
}

func tryFrenchText(raw string) (time.Time, string, bool) {
	parts := strings.Fields(raw)
	if len(parts) != 3 {
		return time.Time{}, "", false
	}
	d, err := strconv.Atoi(parts[0])
	if err != nil || d < 1 || d > 31 {
		return time.Time{}, "", false
	}
	mo, ok := frMonths[strings.ToLower(strings.TrimRight(parts[1], "."))]
	if !ok {
		return time.Time{}, "", false
	}
	y, err := strconv.Atoi(parts[2])
	if err != nil {
		return time.Time{}, "", false
	}
	t, valid := buildDate(y, mo, d, 0, 0, 0)
	if !valid {
		return time.Time{}, "", false
	}
	return t, "d MMMM yyyy", true
}

func tryEnglishText(raw string) (time.Time, string, bool) {
	// "Jan 15, 2024" or "January 15, 2024"
	s := strings.ReplaceAll(raw, ",", "")
	parts := strings.Fields(s)
	if len(parts) != 3 {
		return time.Time{}, "", false
	}
	mo, ok := enMonths[strings.ToLower(parts[0])]
	if !ok {
		return time.Time{}, "", false
	}
	d, err := strconv.Atoi(parts[1])
	if err != nil || d < 1 || d > 31 {
		return time.Time{}, "", false
	}
	y, err := strconv.Atoi(parts[2])
	if err != nil {
		return time.Time{}, "", false
	}
	t, valid := buildDate(y, mo, d, 0, 0, 0)
	if !valid {
		return time.Time{}, "", false
	}
	return t, "MMM d, yyyy", true
}

func tryAMPM(raw string, hint Hint) (time.Time, string, bool) {
	m := ampmRe.FindStringSubmatch(raw)
	if m == nil {
		return time.Time{}, "", false
	}
	p1, _ := strconv.Atoi(m[1])
	p2, _ := strconv.Atoi(m[2])
	var mo, d int
	if hint == HintMDY {
		mo, d = p1, p2
	} else {
		d, mo = p1, p2
	}
	y, _ := strconv.Atoi(m[3])
	H, _ := strconv.Atoi(m[4])
	min, _ := strconv.Atoi(m[5])
	s := 0
	if m[6] != "" {
		s, _ = strconv.Atoi(m[6])
	}
	if strings.ToUpper(m[7]) == "PM" && H != 12 {
		H += 12
	} else if strings.ToUpper(m[7]) == "AM" && H == 12 {
		H = 0
	}
	t, ok := buildDate(y, mo, d, H, min, s)
	if !ok {
		return time.Time{}, "", false
	}
	return t, "M/d/yyyy h:mm:ss a", true
}

func tryGoStdDate(raw string) (time.Time, string, bool) {
	for _, layout := range goStdLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, "EEE MMM dd HH:mm:ss z yyyy", true
		}
	}
	return time.Time{}, "", false
}

// Normalize converts any supported date string to "dd-MM-yyyy HH:mm:ss".
// hint disambiguates numeric d/m vs m/d formats.
// cachedFormat is an optional previously matched format to skip the full chain.
func Normalize(value string, hint Hint, _ string) Result {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return Result{Normalized: value}
	}

	// Unix timestamps (unambiguous)
	if unixMillisRe.MatchString(trimmed) {
		ms, _ := strconv.ParseInt(trimmed, 10, 64)
		return Result{Normalized: fmt2(time.UnixMilli(ms)), WasParsed: true, MatchedFormat: "unix-ms"}
	}
	if unixSecondsRe.MatchString(trimmed) {
		sec, _ := strconv.ParseInt(trimmed, 10, 64)
		return Result{Normalized: fmt2(time.Unix(sec, 0)), WasParsed: true, MatchedFormat: "unix-s"}
	}

	// ISO (unambiguous)
	if t, label, ok := tryISO(trimmed); ok {
		return Result{Normalized: fmt2(t), WasParsed: true, MatchedFormat: label}
	}

	switch hint {
	case HintDMY:
		if t, label, ok := tryNumericDMY(trimmed); ok {
			return Result{Normalized: fmt2(t), WasParsed: true, MatchedFormat: label}
		}
		if t, label, ok := tryFrenchText(trimmed); ok {
			return Result{Normalized: fmt2(t), WasParsed: true, MatchedFormat: label}
		}
	default: // MDY
		if t, label, ok := tryNumericMDY(trimmed); ok {
			return Result{Normalized: fmt2(t), WasParsed: true, MatchedFormat: label}
		}
		if t, label, ok := tryAMPM(trimmed, hint); ok {
			return Result{Normalized: fmt2(t), WasParsed: true, MatchedFormat: label}
		}
		if t, label, ok := tryEnglishText(trimmed); ok {
			return Result{Normalized: fmt2(t), WasParsed: true, MatchedFormat: label}
		}
		if t, label, ok := tryGoStdDate(trimmed); ok {
			return Result{Normalized: fmt2(t), WasParsed: true, MatchedFormat: label}
		}
	}

	return Result{Normalized: trimmed}
}
