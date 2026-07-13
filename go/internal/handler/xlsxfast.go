package handler

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"arteci-go/internal/dateparser"
)

// fastXLSX processes an XLSX file by streaming ZIP/XML without excelize's full model.
// Normalizes date columns and writes a modified XLSX to out. r+size back a
// spilled-to-disk temp file (see spillToTempFile) rather than an in-memory
// buffer, since archive/zip needs random access but the file may be large.
func fastXLSX(
	r io.ReaderAt,
	size int64,
	dateColumns []string,
	hints []dateparser.Hint,
	pMax int,
	out io.Writer,
) (preview []map[string]string, totalRows int64, totalFailed int64, err error) {

	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("xlsx: open zip: %w", err)
	}

	const sheetPath = "xl/worksheets/sheet1.xml"

	var ss []string
	var sheetFile *zip.File
	for _, f := range zr.File {
		switch f.Name {
		case "xl/sharedStrings.xml":
			rc, e := f.Open()
			if e == nil {
				ss, _ = xlsxParseSST(rc)
				rc.Close()
			}
		case sheetPath:
			sheetFile = f
		}
	}
	if sheetFile == nil {
		return nil, 0, 0, fmt.Errorf("xlsx: %s not found", sheetPath)
	}

	zw := zip.NewWriter(out)

	// Copy all entries except sheet1 verbatim (no decompression overhead)
	for _, f := range zr.File {
		if f.Name == sheetPath {
			continue
		}
		w, e := zw.CreateRaw(&f.FileHeader)
		if e != nil {
			return nil, 0, 0, fmt.Errorf("xlsx: zip create %s: %w", f.Name, e)
		}
		src, e := f.OpenRaw()
		if e != nil {
			return nil, 0, 0, fmt.Errorf("xlsx: zip open raw %s: %w", f.Name, e)
		}
		_, e = io.Copy(w, src)
		if e != nil {
			return nil, 0, 0, fmt.Errorf("xlsx: zip copy %s: %w", f.Name, e)
		}
	}

	// Stream-normalize sheet1.xml through a pipe to avoid buffering the full XML
	pr, pw := io.Pipe()
	var normalizeErr error

	go func() {
		rc, e := sheetFile.Open()
		if e != nil {
			pw.CloseWithError(e)
			return
		}
		defer rc.Close()
		preview, totalRows, totalFailed, normalizeErr = xlsxStreamSheet(rc, pw, ss, dateColumns, hints, pMax)
		if normalizeErr != nil {
			pw.CloseWithError(normalizeErr)
		} else {
			pw.Close()
		}
	}()

	sheetEntry, e := zw.Create(sheetPath)
	if e != nil {
		pr.CloseWithError(e)
		return nil, 0, 0, fmt.Errorf("xlsx: zip create sheet: %w", e)
	}
	if _, e = io.Copy(sheetEntry, pr); e != nil {
		return nil, 0, 0, fmt.Errorf("xlsx: zip write sheet: %w", e)
	}
	if normalizeErr != nil {
		return nil, 0, 0, normalizeErr
	}
	if e := zw.Close(); e != nil {
		return nil, 0, 0, fmt.Errorf("xlsx: zip close: %w", e)
	}
	return preview, totalRows, totalFailed, nil
}

// xlsxStreamSheet reads sheet XML, normalizes date cells, writes transformed XML to w.
func xlsxStreamSheet(
	r io.Reader,
	w io.Writer,
	ss []string,
	dateColumns []string,
	hints []dateparser.Hint,
	pMax int,
) (preview []map[string]string, totalRows int64, totalFailed int64, err error) {

	dec := xml.NewDecoder(r)
	enc := xml.NewEncoder(w)

	// Built from header row, maps colIdx → hint index
	dateHint := make(map[int]int)
	colCaches := make([]map[int]string, len(hints))
	for i := range colCaches {
		colCaches[i] = make(map[int]string)
	}
	var headers []string

	var (
		rowNum int

		// date cell
		inDate      bool
		dateRef     string
		dateTyp     string
		dateVal     strings.Builder
		inV         bool
		inIS        bool
		inIST       bool

		// non-date cell (tracked for header row + preview rows)
		trackRow      bool
		inOther       bool
		otherRef      string
		otherTyp      string
		otherVal      strings.Builder
		inOtherV      bool
		inOtherIS     bool
		inOtherIST    bool

		curRow map[int]string
	)

	for {
		tok, terr := dec.Token()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			_ = enc.Flush()
			return nil, 0, 0, fmt.Errorf("xlsx xml: %w", terr)
		}

		// xml.Encoder re-declares xmlns from Name.Space on every element it
		// doesn't already know about via EncodeElement — since this decodes
		// and re-encodes tokens one at a time (never EncodeElement), that
		// means every element ends up with a redundant xmlns, and the root
		// element (whose original Attr already has an explicit xmlns) gets
		// it twice, which strict XML parsers (e.g. Excel, openpyxl) reject
		// as a duplicate attribute. The default namespace is already
		// declared once on <worksheet> and inherited by every descendant,
		// so drop Name.Space before re-encoding instead of letting the
		// encoder synthesize its own declaration.
		switch st := tok.(type) {
		case xml.StartElement:
			st.Name.Space = ""
			tok = st
		case xml.EndElement:
			st.Name.Space = ""
			tok = st
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				rowNum++
				trackRow = rowNum == 1 || totalRows < int64(pMax)
				if trackRow {
					curRow = make(map[int]string)
				}
				enc.EncodeToken(tok)

			case "c":
				ref := xlsxAttr(t.Attr, "r")
				ci := xlsxCellCol(ref)
				hi, isDate := dateHint[ci]
				_ = hi
				if isDate && rowNum > 1 {
					inDate = true
					dateRef = ref
					dateTyp = xlsxAttr(t.Attr, "t")
					dateVal.Reset()
				} else {
					enc.EncodeToken(tok)
					if trackRow {
						inOther = true
						otherRef = ref
						otherTyp = xlsxAttr(t.Attr, "t")
						otherVal.Reset()
					}
				}

			case "v":
				if inDate {
					inV = true
				} else {
					if inOther {
						inOtherV = true
					}
					enc.EncodeToken(tok)
				}
			case "is":
				if inDate {
					inIS = true
				} else {
					if inOther {
						inOtherIS = true
					}
					enc.EncodeToken(tok)
				}
			case "t":
				if inDate && inIS {
					inIST = true
				} else {
					if inOther && inOtherIS {
						inOtherIST = true
					}
					enc.EncodeToken(tok)
				}
			default:
				enc.EncodeToken(tok)
			}

		case xml.EndElement:
			switch t.Name.Local {
			case "row":
				if rowNum == 1 {
					// Build headers from decoded cells
					maxCol := 0
					for ci := range curRow {
						if ci > maxCol {
							maxCol = ci
						}
					}
					headers = make([]string, maxCol+1)
					for ci, v := range curRow {
						headers[ci] = v
					}
					// Map dateColumns → colIdx
					hIdx := make(map[string]int, len(headers))
					for i, h := range headers {
						hIdx[strings.TrimSpace(h)] = i
					}
					for i, col := range dateColumns {
						idx, ok := hIdx[col]
						if !ok {
							_ = enc.Flush()
							return nil, 0, 0, fmt.Errorf("Column '%s' not found. Available: %s",
								col, strings.Join(headers, ", "))
						}
						dateHint[idx] = i
					}
				} else {
					totalRows++
					if trackRow {
						row := make(map[string]string, len(headers))
						for ci, v := range curRow {
							if ci < len(headers) {
								row[headers[ci]] = v
							}
						}
						preview = append(preview, row)
					}
				}
				enc.EncodeToken(tok)

			case "c":
				if inDate {
					raw := dateVal.String()
					if dateTyp == "s" {
						raw = xlsxSSVal(ss, raw)
					}
					ci := xlsxCellCol(dateRef)
					hi := dateHint[ci]
					res := dateparser.Normalize(raw, hints[hi], colCaches[hi][ci])
					if res.MatchedFormat != "" {
						colCaches[hi][ci] = res.MatchedFormat
					}
					if !res.WasParsed && strings.TrimSpace(raw) != "" {
						totalFailed++
					}
					if trackRow {
						curRow[ci] = res.Normalized
					}
					enc.EncodeToken(xml.StartElement{
						Name: xml.Name{Local: "c"},
						Attr: []xml.Attr{
							{Name: xml.Name{Local: "r"}, Value: dateRef},
							{Name: xml.Name{Local: "t"}, Value: "inlineStr"},
						},
					})
					enc.EncodeToken(xml.StartElement{Name: xml.Name{Local: "is"}})
					enc.EncodeToken(xml.StartElement{Name: xml.Name{Local: "t"}})
					enc.EncodeToken(xml.CharData(res.Normalized))
					enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: "t"}})
					enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: "is"}})
					enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: "c"}})
					inDate = false
					inV = false
					inIS = false
					inIST = false
				} else if inOther {
					if trackRow {
						raw := otherVal.String()
						if otherTyp == "s" {
							raw = xlsxSSVal(ss, raw)
						}
						curRow[xlsxCellCol(otherRef)] = raw
					}
					inOther = false
					inOtherV = false
					inOtherIS = false
					inOtherIST = false
					enc.EncodeToken(tok)
				} else {
					enc.EncodeToken(tok)
				}

			case "v":
				if inDate {
					inV = false
				} else {
					if inOther {
						inOtherV = false
					}
					enc.EncodeToken(tok)
				}
			case "is":
				if inDate {
					inIS = false
				} else {
					if inOther {
						inOtherIS = false
					}
					enc.EncodeToken(tok)
				}
			case "t":
				if inDate && inIS {
					inIST = false
				} else {
					if inOther && inOtherIS {
						inOtherIST = false
					}
					enc.EncodeToken(tok)
				}
			default:
				enc.EncodeToken(tok)
			}

		case xml.CharData:
			if inDate && (inV || inIST) {
				dateVal.Write(t)
			} else if inOther && (inOtherV || inOtherIST) {
				otherVal.Write(t)
				enc.EncodeToken(tok)
			} else {
				enc.EncodeToken(tok)
			}

		default:
			enc.EncodeToken(tok)
		}
	}

	return preview, totalRows, totalFailed, enc.Flush()
}

// xlsxReadFirstRow reads only the header row from sheet1.xml.
func xlsxReadFirstRow(r io.Reader, ss []string) ([]string, error) {
	dec := xml.NewDecoder(r)
	var (
		rowNum  int
		inCell  bool
		cellRef string
		cellTyp string
		cellVal strings.Builder
		inV     bool
		inIS    bool
		inIST   bool
		cells   map[int]string
	)
	cells = make(map[int]string)

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				rowNum++
				if rowNum > 1 {
					goto done
				}
			case "c":
				if rowNum == 1 {
					inCell = true
					cellRef = xlsxAttr(t.Attr, "r")
					cellTyp = xlsxAttr(t.Attr, "t")
					cellVal.Reset()
				}
			case "v":
				if inCell {
					inV = true
				}
			case "is":
				if inCell {
					inIS = true
				}
			case "t":
				if inCell && inIS {
					inIST = true
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "row":
				if rowNum == 1 {
					goto done
				}
			case "c":
				if inCell {
					raw := cellVal.String()
					if cellTyp == "s" {
						raw = xlsxSSVal(ss, raw)
					}
					cells[xlsxCellCol(cellRef)] = raw
					inCell = false
					inV = false
					inIS = false
					inIST = false
				}
			case "v":
				inV = false
			case "is":
				inIS = false
			case "t":
				inIST = false
			}
		case xml.CharData:
			if inCell && (inV || inIST) {
				cellVal.Write(t)
			}
		}
	}

done:
	if len(cells) == 0 {
		return nil, fmt.Errorf("empty or unreadable sheet")
	}
	maxCol := 0
	for ci := range cells {
		if ci > maxCol {
			maxCol = ci
		}
	}
	result := make([]string, maxCol+1)
	for ci, v := range cells {
		result[ci] = v
	}
	return result, nil
}

// xlsxParseSST parses xl/sharedStrings.xml into a string slice.
func xlsxParseSST(r io.Reader) ([]string, error) {
	var result []string
	var buf strings.Builder
	var inSI, inT bool

	dec := xml.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "si" {
				inSI = true
				buf.Reset()
			} else if inSI && t.Name.Local == "t" {
				inT = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inT = false
			} else if t.Name.Local == "si" {
				result = append(result, buf.String())
				inSI = false
			}
		case xml.CharData:
			if inT {
				buf.Write(t)
			}
		}
	}
	return result, nil
}

// xlsxSSVal resolves a shared string index ("42") to the actual string value.
func xlsxSSVal(ss []string, idxStr string) string {
	n := 0
	for _, d := range idxStr {
		if d < '0' || d > '9' {
			return idxStr
		}
		n = n*10 + int(d-'0')
	}
	if n < len(ss) {
		return ss[n]
	}
	return idxStr
}

// xlsxCellCol returns the 0-based column index from a cell ref ("A1"→0, "C3"→2).
func xlsxCellCol(ref string) int {
	n := 0
	for _, c := range ref {
		if c < 'A' || c > 'Z' {
			break
		}
		n = n*26 + int(c-'A'+1)
	}
	return n - 1
}

// xlsxAttr returns an XML attribute value by local name.
func xlsxAttr(attrs []xml.Attr, local string) string {
	for _, a := range attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}
