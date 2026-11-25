package pptx

import _ "embed"

// The XML parts used to be generated from these text/template sources. They
// are now written by the functions in parts.go, which produce the same bytes
// without text/template; parts_test.go still executes these sources to check
// that. They stay exported for one compatibility release.

// Deprecated: PPTX export no longer uses this template, so changing it has no
// effect. It will be removed after one compatibility release.
//
//go:embed templates/content_types.xml
var CONTENT_TYPES_XML string

// Deprecated: PPTX export no longer uses this template, so changing it has no
// effect. It will be removed after one compatibility release.
//
//go:embed templates/slide.xml.rels
var RELS_SLIDE_XML string

// Deprecated: PPTX export no longer uses this template, so changing it has no
// effect. It will be removed after one compatibility release.
//
//go:embed templates/slide.xml
var SLIDE_XML string

// Deprecated: PPTX export no longer uses this template, so changing it has no
// effect. It will be removed after one compatibility release.
//
//go:embed templates/rels_presentation.xml
var RELS_PRESENTATION_XML string

// Deprecated: PPTX export no longer uses this template, so changing it has no
// effect. It will be removed after one compatibility release.
//
//go:embed templates/presentation.xml
var PRESENTATION_XML string

// Deprecated: PPTX export no longer uses this template, so changing it has no
// effect. It will be removed after one compatibility release.
//
//go:embed templates/core.xml
var CORE_XML string

// Deprecated: PPTX export no longer uses this template, so changing it has no
// effect. It will be removed after one compatibility release.
//
//go:embed templates/app.xml
var APP_XML string
