package handler

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"arteci-go/internal/dateparser"
)

func TestXlsxCellCol(t *testing.T) {
	cases := []struct {
		ref  string
		want int
	}{
		{"A1", 0},
		{"B1", 1},
		{"Z1", 25},
		{"AA1", 26},
		{"AB10", 27},
	}
	for _, tc := range cases {
		if got := xlsxCellCol(tc.ref); got != tc.want {
			t.Errorf("xlsxCellCol(%q) = %d, want %d", tc.ref, got, tc.want)
		}
	}
}

func TestXlsxAttr(t *testing.T) {
	attrs := []xml.Attr{
		{Name: xml.Name{Local: "r"}, Value: "B2"},
		{Name: xml.Name{Local: "t"}, Value: "s"},
	}
	if got := xlsxAttr(attrs, "t"); got != "s" {
		t.Errorf("xlsxAttr(t) = %q, want %q", got, "s")
	}
	if got := xlsxAttr(attrs, "missing"); got != "" {
		t.Errorf("xlsxAttr(missing) = %q, want empty", got)
	}
}

func TestXlsxSSVal(t *testing.T) {
	ss := []string{"foo", "bar", "baz"}
	if got := xlsxSSVal(ss, "1"); got != "bar" {
		t.Errorf("xlsxSSVal(1) = %q, want %q", got, "bar")
	}
	if got := xlsxSSVal(ss, "99"); got != "99" {
		t.Errorf("xlsxSSVal(99) = %q, want %q", got, "99")
	}
	if got := xlsxSSVal(ss, "abc"); got != "abc" {
		t.Errorf("xlsxSSVal(abc) = %q, want %q", got, "abc")
	}
}

func TestXlsxParseSST(t *testing.T) {
	sst := `<?xml version="1.0"?>
<sst><si><t>Alice</t></si><si><t>Bob</t></si></sst>`
	ss, err := xlsxParseSST(strings.NewReader(sst))
	if err != nil {
		t.Fatalf("xlsxParseSST: %v", err)
	}
	if len(ss) != 2 || ss[0] != "Alice" || ss[1] != "Bob" {
		t.Errorf("xlsxParseSST = %v, want [Alice Bob]", ss)
	}
}

func TestXlsxReadFirstRow(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<worksheet><sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>NAME</t></is></c><c r="B1" t="inlineStr"><is><t>DATE_CREATION</t></is></c></row>
<row r="2"><c r="A2" t="inlineStr"><is><t>Alice</t></is></c></row>
</sheetData></worksheet>`
	headers, err := xlsxReadFirstRow(strings.NewReader(sheet), nil)
	if err != nil {
		t.Fatalf("xlsxReadFirstRow: %v", err)
	}
	want := []string{"NAME", "DATE_CREATION"}
	if len(headers) != len(want) {
		t.Fatalf("headers = %v, want %v", headers, want)
	}
	for i := range want {
		if headers[i] != want[i] {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], want[i])
		}
	}
}

func TestXlsxStreamSheet(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<worksheet><sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>NAME</t></is></c><c r="B1" t="inlineStr"><is><t>DATE_CREATION</t></is></c></row>
<row r="2"><c r="A2" t="inlineStr"><is><t>Alice</t></is></c><c r="B2" t="inlineStr"><is><t>07/17/2019</t></is></c></row>
<row r="3"><c r="A3" t="inlineStr"><is><t>Bob</t></is></c><c r="B3" t="inlineStr"><is><t>not-a-date</t></is></c></row>
</sheetData></worksheet>`

	var out bytes.Buffer
	preview, totalRows, totalFailed, err := xlsxStreamSheet(
		strings.NewReader(sheet), &out, nil,
		[]string{"DATE_CREATION"}, []dateparser.Hint{dateparser.HintMDY}, 100,
	)
	if err != nil {
		t.Fatalf("xlsxStreamSheet: %v", err)
	}
	if totalRows != 2 {
		t.Errorf("totalRows = %d, want 2", totalRows)
	}
	if totalFailed != 1 {
		t.Errorf("totalFailed = %d, want 1 (the 'not-a-date' cell)", totalFailed)
	}
	if len(preview) != 2 {
		t.Fatalf("preview rows = %d, want 2", len(preview))
	}
	if preview[0]["DATE_CREATION"] != "17-07-2019 00:00:00" {
		t.Errorf("preview[0][DATE_CREATION] = %q, want %q", preview[0]["DATE_CREATION"], "17-07-2019 00:00:00")
	}
	if preview[1]["DATE_CREATION"] != "not-a-date" {
		t.Errorf("preview[1][DATE_CREATION] = %q, want unparsed value returned as-is", preview[1]["DATE_CREATION"])
	}
	if !strings.Contains(out.String(), "17-07-2019 00:00:00") {
		t.Errorf("output XML missing normalized date, got: %s", out.String())
	}
}

func TestXlsxStreamSheet_UnknownColumn(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<worksheet><sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>NAME</t></is></c></row>
</sheetData></worksheet>`
	var out bytes.Buffer
	_, _, _, err := xlsxStreamSheet(
		strings.NewReader(sheet), &out, nil,
		[]string{"DATE_CREATION"}, []dateparser.Hint{dateparser.HintMDY}, 100,
	)
	if err == nil {
		t.Fatal("expected an error for a date column absent from the header row")
	}
}


func TestFastXLSX_RoundTrip(t *testing.T) {
	sheet := `<?xml version="1.0"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>NAME</t></is></c><c r="B1" t="inlineStr"><is><t>DATE_CREATION</t></is></c></row>
<row r="2"><c r="A2" t="inlineStr"><is><t>Alice</t></is></c><c r="B2" t="inlineStr"><is><t>07/17/2019</t></is></c></row>
</sheetData></worksheet>`

	var srcBuf bytes.Buffer
	zw := zip.NewWriter(&srcBuf)
	w1, _ := zw.Create("xl/worksheets/sheet1.xml")
	w1.Write([]byte(sheet))
	w2, _ := zw.Create("[Content_Types].xml")
	w2.Write([]byte("<Types/>"))
	if err := zw.Close(); err != nil {
		t.Fatalf("build fixture zip: %v", err)
	}

	var out bytes.Buffer
	src := bytes.NewReader(srcBuf.Bytes())
	preview, totalRows, totalFailed, err := fastXLSX(
		src, int64(src.Len()), []string{"DATE_CREATION"}, []dateparser.Hint{dateparser.HintMDY}, 100, &out,
	)
	if err != nil {
		t.Fatalf("fastXLSX: %v", err)
	}
	if totalRows != 1 || totalFailed != 0 || len(preview) != 1 {
		t.Fatalf("totalRows=%d totalFailed=%d preview=%d, want 1/0/1", totalRows, totalFailed, len(preview))
	}

	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("output is not a valid zip: %v", err)
	}
	var sawSheet, sawContentTypes bool
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		switch f.Name {
		case "xl/worksheets/sheet1.xml":
			sawSheet = true
			if !strings.Contains(string(data), "17-07-2019 00:00:00") {
				t.Errorf("output sheet1.xml missing normalized date: %s", data)
			}
			if n := strings.Count(string(data), "xmlns="); n != 1 {
				t.Errorf("expected exactly one xmlns declaration, found %d: %s", n, data)
			}
		case "[Content_Types].xml":
			sawContentTypes = true
			if string(data) != "<Types/>" {
				t.Errorf("sibling entry was modified, got %q", data)
			}
		}
	}
	if !sawSheet {
		t.Error("output zip is missing xl/worksheets/sheet1.xml")
	}
	if !sawContentTypes {
		t.Error("output zip is missing the untouched [Content_Types].xml entry")
	}
}
