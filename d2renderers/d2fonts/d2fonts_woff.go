package d2fonts

import _ "embed"

// These SVG data URLs contain WOFFs distinct from the TTFs used for text measurement.

//go:embed encoded/SourceSansPro-Semibold.txt
var sourceSansProSemiboldBase64 string

//go:embed encoded/SourceCodePro-Bold.txt
var sourceCodeProBoldBase64 string

//go:embed encoded/SourceCodePro-Semibold.txt
var sourceCodeProSemiboldBase64 string

//go:embed encoded/SourceCodePro-Italic.txt
var sourceCodeProItalicBase64 string
