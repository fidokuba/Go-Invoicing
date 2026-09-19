package invoice

import _ "embed"

// invoicePDFFont is Liberation Serif Regular, embedded directly into the
// binary via go:embed — no filesystem font lookup, no dependency on a
// developer's local font directory (no /Library/Fonts, no
// C:\Windows\Fonts), so PDF generation works identically after a plain
// `go build` on Windows, macOS, or in a minimal Linux/Docker image with
// no fonts installed at all.
//
// Liberation Serif is distributed under the SIL Open Font License 1.1
// (see assets/fonts/LICENSE-LiberationSerif.txt), which permits exactly
// this kind of embedding and redistribution. It's metrically compatible
// with Times New Roman, giving a professional, readable invoice document
// without requiring a proprietary or non-redistributable font.
//
//go:embed assets/fonts/LiberationSerif-Regular.ttf
var invoicePDFFont []byte
