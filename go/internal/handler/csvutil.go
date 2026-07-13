package handler

import "bytes"


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
