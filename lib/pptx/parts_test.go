package pptx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/png"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"text/template"
)

// The exporter used to execute the templates in templates/ with text/template.
// These tests keep doing that, as the reference: every part, and every
// archive, must come out byte for byte as the templates wrote them.

func TestPartsMatchTemplates(t *testing.T) {
	t.Parallel()

	t.Run("content types", func(t *testing.T) {
		checkPart(t, CONTENT_TYPES_XML, contentTypesXML, func(r *rand.Rand) ContentTypesXmlContent {
			return ContentTypesXmlContent{FileNames: randomSlice(r, randomString)}
		})
	})
	t.Run("slide relationships", func(t *testing.T) {
		checkPart(t, RELS_SLIDE_XML, slideRelsXML, func(r *rand.Rand) RelsSlideXmlContent {
			return RelsSlideXmlContent{
				FileName:       randomString(r),
				RelationshipID: randomString(r),
				Links: randomSlice(r, func(r *rand.Rand) RelsSlideXmlLinkContent {
					link := RelsSlideXmlLinkContent{
						RelationshipID: randomString(r),
						SlideIndex:     randomInt(r),
					}
					if r.IntN(2) == 0 {
						link.ExternalUrl = randomString(r)
					}
					return link
				}),
			}
		})
	})
	t.Run("slide", func(t *testing.T) {
		checkPart(t, SLIDE_XML, slideXML, func(r *rand.Rand) SlideXmlContent {
			return SlideXmlContent{
				Title: randomString(r),
				TitlePrefix: randomSlice(r, func(r *rand.Rand) SlideXmlTitlePathContent {
					return SlideXmlTitlePathContent{Name: randomString(r), RelationshipID: randomString(r)}
				}),
				Description:  randomString(r),
				HeaderHeight: randomInt(r),
				ImageID:      randomString(r),
				ImageLeft:    randomInt(r),
				ImageTop:     randomInt(r),
				ImageWidth:   randomInt(r),
				ImageHeight:  randomInt(r),
				Links: randomSlice(r, func(r *rand.Rand) SlideLinkXmlContent {
					link := SlideLinkXmlContent{
						ID:             randomInt(r),
						RelationshipID: randomString(r),
						Name:           randomString(r),
						Left:           randomInt(r),
						Top:            randomInt(r),
						Width:          randomInt(r),
						Height:         randomInt(r),
					}
					switch r.IntN(3) {
					case 0:
						link.Action = "ppaction://hlinksldjump"
					case 1:
						link.Action = randomString(r)
					}
					return link
				}),
			}
		})
	})
	t.Run("presentation relationships", func(t *testing.T) {
		checkPart(t, RELS_PRESENTATION_XML, presentationRelsXML, func(r *rand.Rand) RelsPresentationXmlContent {
			return RelsPresentationXmlContent{
				Slides: randomSlice(r, func(r *rand.Rand) RelsPresentationSlideXmlContent {
					return RelsPresentationSlideXmlContent{RelationshipID: randomString(r), FileName: randomString(r)}
				}),
			}
		})
	})
	t.Run("presentation", func(t *testing.T) {
		checkPart(t, PRESENTATION_XML, presentationXML, func(r *rand.Rand) PresentationXmlContent {
			return PresentationXmlContent{
				SlideWidth:  randomInt(r),
				SlideHeight: randomInt(r),
				Slides: randomSlice(r, func(r *rand.Rand) PresentationSlideXmlContent {
					return PresentationSlideXmlContent{ID: randomInt(r), RelationshipID: randomString(r)}
				}),
			}
		})
	})
	t.Run("core properties", func(t *testing.T) {
		checkPart(t, CORE_XML, coreXML, func(r *rand.Rand) CoreXmlContent {
			return CoreXmlContent{
				Title:          randomString(r),
				Subject:        randomString(r),
				Creator:        randomString(r),
				Description:    randomString(r),
				LastModifiedBy: randomString(r),
			}
		})
	})
	t.Run("app properties", func(t *testing.T) {
		checkPart(t, APP_XML, appXML, func(r *rand.Rand) AppXmlContent {
			return AppXmlContent{
				SlideCount:         randomInt(r),
				TitlesOfPartsCount: randomInt(r),
				Titles:             randomSlice(r, randomString),
				D2Version:          randomString(r),
			}
		})
	})
}

// checkPart compares write with the template for the zero value and for 2000
// random values of its content.
func checkPart[T any](t *testing.T, source string, write func(T) []byte, random func(*rand.Rand) T) {
	t.Helper()
	t.Parallel()

	var zero T
	contents := []T{zero}
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		contents = append(contents, random(r))
	}

	tmpl := parseTemplate(t, source)
	for i, content := range contents {
		var want bytes.Buffer
		if err := tmpl.Execute(&want, content); err != nil {
			t.Fatal(err)
		}
		if got := write(content); !bytes.Equal(got, want.Bytes()) {
			t.Fatalf("value %d: %s\ncontent: %#v", i, firstDifference(got, want.Bytes()), content)
		}
	}
}

func TestStaticPartsMatchTemplates(t *testing.T) {
	t.Parallel()

	for _, source := range []string{ROOT_RELS_XML, SLIDEMASTER_RELS_XML} {
		var want bytes.Buffer
		if err := parseTemplate(t, source).Execute(&want, nil); err != nil {
			t.Fatal(err)
		}
		if source != want.String() {
			t.Fatalf("%s", firstDifference([]byte(source), want.Bytes()))
		}
	}
}

func TestArchiveMatchesTemplates(t *testing.T) {
	t.Parallel()

	var images [][]byte
	for _, size := range []image.Point{{1, 1}, {16, 9}, {9, 16}, {400, 3}, {3, 400}, {1000, 997}} {
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, image.NewGray(image.Rect(0, 0, size.X, size.Y))); err != nil {
			t.Fatal(err)
		}
		images = append(images, encoded.Bytes())
	}

	r := rand.New(rand.NewPCG(3, 4))
	for i := range 100 {
		p := randomPresentation(t, r, images)
		var got bytes.Buffer
		if err := p.ExportTo(&got); err != nil {
			t.Fatal(err)
		}
		if want := exportWithTemplates(t, p); !bytes.Equal(got.Bytes(), want) {
			t.Fatalf("presentation %d: %s", i, firstDifference(got.Bytes(), want))
		}
	}
}

func randomPresentation(t *testing.T, r *rand.Rand, images [][]byte) *Presentation {
	t.Helper()

	p := NewPresentation(randomString(r), randomString(r), randomString(r), randomString(r), randomString(r), r.IntN(2) == 0)
	slides := r.IntN(5)
	if r.IntN(10) == 0 {
		slides = 100 + r.IntN(50)
	}
	for range slides {
		path := make([]BoardTitle, 1+r.IntN(4))
		for i := range path {
			path[i] = BoardTitle{Name: randomString(r), BoardID: randomString(r), LinkToSlide: r.IntN(slides + 1)}
		}
		slide, err := p.AddSlide(images[r.IntN(len(images))], path)
		if err != nil {
			t.Fatal(err)
		}
		for range r.IntN(5) {
			link := &Link{
				Top:     r.IntN(2000),
				Left:    r.IntN(2000),
				Width:   r.IntN(2000),
				Height:  r.IntN(2000),
				Tooltip: randomString(r),
			}
			if r.IntN(2) == 0 {
				link.ExternalUrl = randomString(r)
			} else {
				link.SlideIndex = 1 + r.IntN(slides)
			}
			slide.AddLink(link)
		}
	}
	return p
}

// exportWithTemplates writes p as the exporter did before its parts were
// written in Go: the same archive, with every XML part executed from its
// template straight into the archive entry.
func exportWithTemplates(t *testing.T, p *Presentation) []byte {
	t.Helper()

	var out bytes.Buffer
	zipWriter := zip.NewWriter(&out)
	templates := make(map[string]*template.Template)
	add := func(name, source string, data any) {
		w, err := zipWriter.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if templates[source] == nil {
			templates[source] = parseTemplate(t, source)
		}
		if err := templates[source].Execute(w, data); err != nil {
			t.Fatal(err)
		}
	}

	var slideFileNames []string
	for i := range p.Slides {
		slideFileNames = append(slideFileNames, fmt.Sprintf("slide%d", i+1))
	}
	add("[Content_Types].xml", CONTENT_TYPES_XML, ContentTypesXmlContent{FileNames: slideFileNames})
	if err := copyPptxTemplateTo(zipWriter); err != nil {
		t.Fatal(err)
	}
	add("_rels/.rels", ROOT_RELS_XML, nil)
	add("ppt/slideMasters/_rels/slideMaster1.xml.rels", SLIDEMASTER_RELS_XML, nil)
	for i, slide := range p.Slides {
		imageID := fmt.Sprintf("slide%dImage", i+1)
		w, err := zipWriter.Create(fmt.Sprintf("ppt/media/%s.png", imageID))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(slide.Image); err != nil {
			t.Fatal(err)
		}
		add(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i+1), RELS_SLIDE_XML, getSlideXmlRelsContent(imageID, slide, i+1))
		add(fmt.Sprintf("ppt/slides/slide%d.xml", i+1), SLIDE_XML, p.getSlideXmlContent(imageID, slide))
	}
	add("ppt/_rels/presentation.xml.rels", RELS_PRESENTATION_XML, getRelsPresentationXmlContent(slideFileNames))
	add("ppt/presentation.xml", PRESENTATION_XML, getPresentationXmlContent(slideFileNames))
	add("docProps/core.xml", CORE_XML, CoreXmlContent{
		Creator:        p.Creator,
		Subject:        p.Subject,
		Description:    p.Description,
		LastModifiedBy: p.Creator,
		Title:          p.Title,
	})
	titles := make([]string, 0, len(p.Slides))
	for _, slide := range p.Slides {
		titles = append(titles, slide.BoardTitle[len(slide.BoardTitle)-1].BoardID)
	}
	add("docProps/app.xml", APP_XML, AppXmlContent{
		SlideCount:         len(p.Slides),
		TitlesOfPartsCount: len(p.Slides) + 3,
		D2Version:          p.D2Version,
		Titles:             titles,
	})
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// parseTemplate parses one of the former templates with the functions the
// exporter gave them.
func parseTemplate(t *testing.T, source string) *template.Template {
	t.Helper()

	tmpl, err := template.New("").Funcs(template.FuncMap{
		"xmlAttr": templateEscapeXML,
		"xmlText": templateEscapeXML,
	}).Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

// templateEscapeXML is how the templates escaped every value, strings and
// integers alike.
func templateEscapeXML(value any) (string, error) {
	var escaped strings.Builder
	err := xml.EscapeText(&escaped, []byte(fmt.Sprint(value)))
	return escaped.String(), err
}

// specialStrings exercise escaping: XML syntax, quotes, white space, non-ASCII
// text, invalid UTF-8, characters that XML cannot represent, template and
// format verbs, and a long value.
var specialStrings = []string{
	"",
	"slide1",
	`A & B < C > D "quoted" 'apostrophe'`,
	"&amp; &lt;already&gt; &#34;escaped&#34;",
	"tab\tline feed\ncarriage return\r\n",
	"héllo wörld, 日本語, 🙂, \u2028\u2029, \ufeffBOM",
	"\ufffd replacement character",
	"invalid UTF-8: \xff \xc3\x28 \xed\xa0\x80 \xf4\x90\x80\x80 \xe2\x82",
	"control characters: \x00 \x01 \x08 \x0b \x0c \x1f \x7f \u0085",
	"not XML characters: \ufffe \uffff \U0010ffff",
	"{{.Title}} {{xmlAttr .}} {{end}}",
	"%s %d %v %% %!(EXTRA) %[1]s",
	"]]> <![CDATA[ x ]]> <!-- comment -->",
	"https://example.com/search?q=a%20b&next=%3Ctag%3E&quote=%22double%22",
	strings.Repeat("long value ", 500),
}

func randomString(r *rand.Rand) string {
	var b []byte
	switch r.IntN(4) {
	case 0:
		return specialStrings[r.IntN(len(specialStrings))]
	case 1: // characters that matter to XML, templates and format strings
		const alphabet = "&<>\"'\t\n\r %{}.\\aZ09-"
		for range r.IntN(40) {
			b = append(b, alphabet[r.IntN(len(alphabet))])
		}
	case 2: // arbitrary bytes, mostly invalid UTF-8
		for range r.IntN(40) {
			b = append(b, byte(r.IntN(256)))
		}
	default: // arbitrary code points
		for range r.IntN(40) {
			b = append(b, string(rune(r.IntN(0x110000)))...)
		}
	}
	return string(b)
}

var specialInts = []int{
	0, 1, -1, 7, 9, 10, 99, 100, 256, -256,
	math.MaxInt32, math.MinInt32, math.MaxInt, math.MinInt,
	SLIDE_WIDTH, SLIDE_HEIGHT, HEADER_HEIGHT, IMAGE_WIDTH,
}

func randomInt(r *rand.Rand) int {
	if r.IntN(2) == 0 {
		return specialInts[r.IntN(len(specialInts))]
	}
	n := int(r.Int64() >> r.IntN(64))
	if r.IntN(2) == 0 {
		n = -n
	}
	return n
}

// randomSlice returns nil, an empty slice, a few items or, rarely, many.
func randomSlice[T any](r *rand.Rand, item func(*rand.Rand) T) []T {
	n := r.IntN(5)
	if r.IntN(10) == 0 {
		n = r.IntN(200)
	}
	if n == 0 && r.IntN(2) == 0 {
		return nil
	}
	s := make([]T, n)
	for i := range s {
		s[i] = item(r)
	}
	return s
}

// firstDifference describes where got first differs from want.
func firstDifference(got, want []byte) string {
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	around := func(b []byte) string {
		return fmt.Sprintf("%q", b[max(0, i-60):min(len(b), i+60)])
	}
	return fmt.Sprintf("output differs from the template at byte %d (lengths %d and %d)\ngot:  %s\nwant: %s",
		i, len(got), len(want), around(got), around(want))
}
