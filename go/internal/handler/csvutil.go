package handler

import "bytes"

// detectDelimiter sniffs the CSV field delimiter from a sample of raw bytes
// (typically the header line) by counting unquoted commas vs semicolons —
// the two conventions this API actually needs to support (en_US vs fr_FR
// exports). Defaults to ';' when the sample is ambiguous or empty, matching
// this project's existing fixtures.
func detectDelimiter(sample []byte) rune {
	if nl := bytes.IndexByte(sample, '\n'); nl >= 0 {
		sample = sample[:nl]
	}
	sample = bytes.TrimRight(sample, "\r")
	commas := bytes.Count(sample, []byte{','})
	semicolons := bytes.Count(sample, []byte{';'})
	if commas > semicolons {
		return ','
	}
	return ';'
}
