package imgbundler

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	tassert "github.com/stretchr/testify/assert"

	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/netpolicy"
	"github.com/d2lang/d2/lib/simplelog"
	"github.com/d2lang/util-go/go2"
)

//go:embed test_png.png
var testPNGFile []byte

const canonicalTestSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 58 58"><rect width="58" height="58"/></svg>`

var httpClient = &http.Client{}

type roundTripFunc func(req *http.Request) *http.Response

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

func bundleRemoteForTest(ctx context.Context, l simplelog.Logger, in []byte, cacheImages bool) ([]byte, error) {
	resolver, err := newLegacyResolver("", localfile.Policy{}, httpClient, netpolicy.Policy{AllowPrivateNetworks: true}, cacheImages)
	if err != nil {
		return in, err
	}
	defer resolver.CloseIdleConnections()
	return BundleWithResolver(ctx, l, in, BundleOptions{Resolver: resolver, Remote: true})
}

func TestRegex(t *testing.T) {
	urls := []string{
		"https://icons.terrastruct.com/essentials/004-picture.svg",
		"http://icons.terrastruct.com/essentials/004-picture.svg",
	}

	notURLs := []string{
		"hi.png",
		"./cat.png",
		"/cat.png",
	}

	for _, href := range append(urls, notURLs...) {
		str := fmt.Sprintf(`<image href="%s" />`, href)
		matches := imageRegex.FindAllStringSubmatch(str, -1)
		if len(matches) != 1 {
			t.Fatalf("uri regex didn't match %s", str)
		}
	}
}

func TestInlineRemote(t *testing.T) {
	ctx := log.With(context.Background(), testlog.New(t))
	svgURL := "https://icons.terrastruct.com/essentials/004-picture.svg"
	pngURL := "https://cdn4.iconfinder.com/data/icons/smart-phones-technologies/512/android-phone.png"

	sampleSVG := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<svg
id="d2-svg"
style="background: white;"
xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"
width="328" height="587" viewBox="-100 -131 328 587"><style type="text/css">
<![CDATA[
.shape {
  shape-rendering: geometricPrecision;
  stroke-linejoin: round;
}
.connection {
  stroke-linecap: round;
  stroke-linejoin: round;
}

]]>
</style><g id="a"><g class="shape" ><image href="%s" x="0" y="0" width="128" height="128" style="fill:#FFFFFF;stroke:#0D32B2;opacity:1.000000;stroke-width:2;" /></g><text class="text-bold" x="64.000000" y="-15.000000" style="text-anchor:middle;font-size:16px;fill:#0A0F25">a</text></g><g id="b"><g class="shape" ><image href="%s" x="0" y="228" width="128" height="128" style="fill:#FFFFFF;stroke:#0D32B2;opacity:1.000000;stroke-width:2;" /></g><text class="text-bold" x="64.000000" y="213.000000" style="text-anchor:middle;font-size:16px;fill:#0A0F25">b</text></g><g id="(a -&gt; b)[0]"><marker id="mk-3990223579" markerWidth="10.000000" markerHeight="12.000000" refX="7.000000" refY="6.000000" viewBox="0.000000 0.000000 10.000000 12.000000" orient="auto" markerUnits="userSpaceOnUse"> <polygon class="connection" fill="#0D32B2" stroke-width="2" points="0.000000,0.000000 10.000000,6.000000 0.000000,12.000000" /> </marker><path d="M 64.000000 130.000000 C 64.000000 168.000000 64.000000 188.000000 64.000000 224.000000" class="connection" style="fill:none;stroke:#0D32B2;opacity:1.000000;stroke-width:2;" marker-end="url(#mk-3990223579)" /></g><style type="text/css"><![CDATA[
.text-bold {
	font-family: "font-bold";
}
@font-face {
	font-family: font-bold;
	src: url("REMOVED");
}]]></style></svg>
`, svgURL, pngURL)

	httpClient.Transport = roundTripFunc(func(req *http.Request) *http.Response {
		respRecorder := httptest.NewRecorder()
		switch req.URL.String() {
		case svgURL:
			respRecorder.WriteString(`<?xml version=\"1.0\" encoding=\"iso-8859-1\"?>\r\n<!-- Generator: Adobe Illustrator 19.0.0, SVG Export Plug-In . SVG Version: 6.00 Build 0)  -->\r\n<svg version=\"1.1\" id=\"Capa_1\" xmlns=\"http://www.w3.org/2000/svg\" xmlns:xlink=\"http://www.w3.org/1999/xlink\" x=\"0px\" y=\"0px\"\r\n\t viewBox=\"0 0 58 58\" style=\"enable-background:new 0 0 58 58;\" xml:space=\"preserve\">\r\n<rect x=\"1\" y=\"7\" style=\"fill:#C3E1ED;stroke:#E7ECED;stroke-width:2;stroke-miterlimit:10;\" width=\"56\" height=\"44\"/>\r\n<circle style=\"fill:#ED8A19;\" cx=\"16\" cy=\"17.569\" r=\"6.569\"/>\r\n<polygon style=\"fill:#1A9172;\" points=\"56,36.111 55,35 43,24 32.5,35.5 37.983,40.983 42,45 56,45 \"/>\r\n<polygon style=\"fill:#1A9172;\" points=\"2,49 26,49 21.983,44.983 11.017,34.017 2,41.956 \"/>\r\n<rect x=\"2\" y=\"45\" style=\"fill:#6B5B4B;\" width=\"54\" height=\"5\"/>\r\n<polygon style=\"fill:#25AE88;\" points=\"37.983,40.983 27.017,30.017 10,45 42,45 \"/>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n</svg>`)
			// The historical fixture above contains literal escaped quotes and is
			// not well-formed XML. Strict imageasset validation deliberately rejects
			// it, so exercise bundling with a canonical supported SVG instead.
			respRecorder.Body.Reset()
			respRecorder.WriteString(canonicalTestSVG)
		case pngURL:
			respRecorder.Write(testPNGFile)
		default:
			t.Fatal(req.URL)
		}
		respRecorder.WriteHeader(200)
		return respRecorder.Result()
	})

	l := simplelog.FromLibLog(ctx)
	out, err := bundleRemoteForTest(ctx, l, []byte(sampleSVG), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "https://") {
		t.Fatal("links still exist")
	}
	if !strings.Contains(string(out), "image/svg+xml") {
		t.Fatal("no svg image inserted")
	}
	if !strings.Contains(string(out), "image/png") {
		t.Fatal("no png image inserted")
	}

	// A response inside the byte ceiling must still be a supported valid image.
	httpClient.Transport = roundTripFunc(func(req *http.Request) *http.Response {
		respRecorder := httptest.NewRecorder()
		bytes := make([]byte, maxImageSize)
		rand.Read(bytes)
		respRecorder.Write(bytes)
		respRecorder.WriteHeader(200)
		return respRecorder.Result()
	})
	_, err = bundleRemoteForTest(ctx, l, []byte(sampleSVG), false)
	if err == nil {
		t.Fatal("expected malformed or unsupported image error")
	}

	// Test too large response
	httpClient.Transport = roundTripFunc(func(req *http.Request) *http.Response {
		respRecorder := httptest.NewRecorder()
		bytes := make([]byte, maxImageSize+1)
		rand.Read(bytes)
		respRecorder.Write(bytes)
		respRecorder.WriteHeader(200)
		return respRecorder.Result()
	})
	_, err = bundleRemoteForTest(ctx, l, []byte(sampleSVG), false)
	if err == nil {
		t.Fatal("expected error")
	}

	// Test error response
	httpClient.Transport = roundTripFunc(func(req *http.Request) *http.Response {
		respRecorder := httptest.NewRecorder()
		respRecorder.WriteHeader(500)
		return respRecorder.Result()
	})
	_, err = bundleRemoteForTest(ctx, l, []byte(sampleSVG), false)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInlineLocal(t *testing.T) {
	ctx := log.With(context.Background(), testlog.New(t))
	svgURL, err := filepath.Abs("./test_svg.svg")
	if err != nil {
		t.Fatal(err)
	}
	pngURL, err := filepath.Abs("./test_png.png")
	if err != nil {
		t.Fatal(err)
	}

	template := `<?xml version="1.0" encoding="utf-8"?>
<svg
id="d2-svg"
style="background: white;"
xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"
width="328" height="587" viewBox="-100 -131 328 587"><style type="text/css">
<![CDATA[
.shape {
  shape-rendering: geometricPrecision;
  stroke-linejoin: round;
}
.connection {
  stroke-linecap: round;
  stroke-linejoin: round;
}

]]>
</style><g id="a"><g class="shape" ><image href="%s" x="0" y="0" width="128" height="128" style="fill:#FFFFFF;stroke:#0D32B2;opacity:1.000000;stroke-width:2;" /></g><text class="text-bold" x="64.000000" y="-15.000000" style="text-anchor:middle;font-size:16px;fill:#0A0F25">a</text></g><g id="b"><g class="shape" ><image href="%s" x="0" y="228" width="128" height="128" style="fill:#FFFFFF;stroke:#0D32B2;opacity:1.000000;stroke-width:2;" /></g><text class="text-bold" x="64.000000" y="213.000000" style="text-anchor:middle;font-size:16px;fill:#0A0F25">b</text></g><g id="(a -&gt; b)[0]"><marker id="mk-3990223579" markerWidth="10.000000" markerHeight="12.000000" refX="7.000000" refY="6.000000" viewBox="0.000000 0.000000 10.000000 12.000000" orient="auto" markerUnits="userSpaceOnUse"> <polygon class="connection" fill="#0D32B2" stroke-width="2" points="0.000000,0.000000 10.000000,6.000000 0.000000,12.000000" /> </marker><path d="M 64.000000 130.000000 C 64.000000 168.000000 64.000000 188.000000 64.000000 224.000000" class="connection" style="fill:none;stroke:#0D32B2;opacity:1.000000;stroke-width:2;" marker-end="url(#mk-3990223579)" /></g><style type="text/css"><![CDATA[
.text-bold {
	font-family: "font-bold";
}
@font-face {
	font-family: font-bold;
	src: url("REMOVED");
}]]></style></svg>
`
	sampleSVG := fmt.Sprintf(template, svgURL, pngURL)

	l := simplelog.FromLibLog(ctx)
	// It doesn't matter what the inputPath is for absolute paths
	out, err := BundleLocalWithPolicy(ctx, l, "asdf", []byte(sampleSVG), localfile.Unrestricted(), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), svgURL) {
		t.Fatal("links still exist")
	}
	if !strings.Contains(string(out), "image/svg+xml") {
		t.Fatal("no svg image inserted")
	}
	if !strings.Contains(string(out), "image/png") {
		t.Fatal("no png image inserted")
	}

	// Relative icon path should be relative to input path
	svgURL = "./test_svg.svg"
	sampleSVG = fmt.Sprintf(template, svgURL, pngURL)

	var erred bool
	l = simplelog.Make(
		go2.Pointer(func(s string) {
			log.Debug(ctx, s)
		}),
		go2.Pointer(func(s string) {
			log.Info(ctx, s)
		}),
		go2.Pointer(func(s string) {
			erred = true
		}),
	)

	// Bogus directory not found
	_, err = BundleLocalWithPolicy(ctx, l, "asdf/asdf/asdf", []byte(sampleSVG), localfile.Unrestricted(), false)
	if err == nil {
		t.Fatal("Expected error for invalid input path")
	}
	if !erred {
		t.Fatal("expected failure")
	}

	// - is ignored
	_, err = BundleLocalWithPolicy(ctx, l, "-", []byte(sampleSVG), localfile.Unrestricted(), false)
	if err != nil {
		t.Fatal(err)
	}

	svgURL = "./test_svg.svg"
	sampleSVG = fmt.Sprintf(template, svgURL, pngURL)

	// correct relative path
	_, err = BundleLocalWithPolicy(ctx, l, "./nested/a.d2", []byte(sampleSVG), localfile.Unrestricted(), false)
	if err != nil {
		t.Fatal(err)
	}
}

// TestDuplicateURL ensures that we don't fetch the same image twice
func TestDuplicateURL(t *testing.T) {
	ctx := log.With(context.Background(), testlog.New(t))
	url1 := "https://icons.terrastruct.com/essentials/004-picture.svg"
	url2 := "https://icons.terrastruct.com/essentials/004-picture.svg"

	sampleSVG := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<svg
id="d2-svg"
style="background: white;"
xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"
width="328" height="587" viewBox="-100 -131 328 587"><style type="text/css">
<![CDATA[
.shape {
  shape-rendering: geometricPrecision;
  stroke-linejoin: round;
}
.connection {
  stroke-linecap: round;
  stroke-linejoin: round;
}

]]>
</style><g id="a"><g class="shape" ><image href="%s" x="0" y="0" width="128" height="128" style="fill:#FFFFFF;stroke:#0D32B2;opacity:1.000000;stroke-width:2;" /></g><text class="text-bold" x="64.000000" y="-15.000000" style="text-anchor:middle;font-size:16px;fill:#0A0F25">a</text></g><g id="b"><g class="shape" ><image href="%s" x="0" y="228" width="128" height="128" style="fill:#FFFFFF;stroke:#0D32B2;opacity:1.000000;stroke-width:2;" /></g><text class="text-bold" x="64.000000" y="213.000000" style="text-anchor:middle;font-size:16px;fill:#0A0F25">b</text></g><g id="(a -&gt; b)[0]"><marker id="mk-3990223579" markerWidth="10.000000" markerHeight="12.000000" refX="7.000000" refY="6.000000" viewBox="0.000000 0.000000 10.000000 12.000000" orient="auto" markerUnits="userSpaceOnUse"> <polygon class="connection" fill="#0D32B2" stroke-width="2" points="0.000000,0.000000 10.000000,6.000000 0.000000,12.000000" /> </marker><path d="M 64.000000 130.000000 C 64.000000 168.000000 64.000000 188.000000 64.000000 224.000000" class="connection" style="fill:none;stroke:#0D32B2;opacity:1.000000;stroke-width:2;" marker-end="url(#mk-3990223579)" /></g><style type="text/css"><![CDATA[
.text-bold {
	font-family: "font-bold";
}
@font-face {
	font-family: font-bold;
	src: url("REMOVED");
}]]></style></svg>
`, url1, url2)

	count := 0

	httpClient.Transport = roundTripFunc(func(req *http.Request) *http.Response {
		count++
		respRecorder := httptest.NewRecorder()
		respRecorder.WriteString(`<?xml version=\"1.0\" encoding=\"iso-8859-1\"?>\r\n<!-- Generator: Adobe Illustrator 19.0.0, SVG Export Plug-In . SVG Version: 6.00 Build 0)  -->\r\n<svg version=\"1.1\" id=\"Capa_1\" xmlns=\"http://www.w3.org/2000/svg\" xmlns:xlink=\"http://www.w3.org/1999/xlink\" x=\"0px\" y=\"0px\"\r\n\t viewBox=\"0 0 58 58\" style=\"enable-background:new 0 0 58 58;\" xml:space=\"preserve\">\r\n<rect x=\"1\" y=\"7\" style=\"fill:#C3E1ED;stroke:#E7ECED;stroke-width:2;stroke-miterlimit:10;\" width=\"56\" height=\"44\"/>\r\n<circle style=\"fill:#ED8A19;\" cx=\"16\" cy=\"17.569\" r=\"6.569\"/>\r\n<polygon style=\"fill:#1A9172;\" points=\"56,36.111 55,35 43,24 32.5,35.5 37.983,40.983 42,45 56,45 \"/>\r\n<polygon style=\"fill:#1A9172;\" points=\"2,49 26,49 21.983,44.983 11.017,34.017 2,41.956 \"/>\r\n<rect x=\"2\" y=\"45\" style=\"fill:#6B5B4B;\" width=\"54\" height=\"5\"/>\r\n<polygon style=\"fill:#25AE88;\" points=\"37.983,40.983 27.017,30.017 10,45 42,45 \"/>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n</svg>`)
		respRecorder.WriteHeader(200)
		respRecorder.Body.Reset()
		respRecorder.WriteString(canonicalTestSVG)
		return respRecorder.Result()
	})

	l := simplelog.FromLibLog(ctx)
	out, err := bundleRemoteForTest(ctx, l, []byte(sampleSVG), false)
	if err != nil {
		t.Fatal(err)
	}
	tassert.Equal(t, 1, count)
	if strings.Contains(string(out), url1) {
		t.Fatal("links still exist")
	}
	tassert.Equal(t, 2, strings.Count(string(out), "image/svg+xml"))
}

func TestInlineRemoteCompressedSVG(t *testing.T) {
	ctx := log.With(context.Background(), testlog.New(t))
	svgURL := "https://icons.terrastruct.com/essentials/004-picture.svg"
	rawSVG := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`)
	sampleSVG := []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg"><image href="%s" /></svg>`, svgURL))

	for _, tc := range []struct {
		name            string
		contentEncoding string
		encode          func(*testing.T, []byte) []byte
	}{
		{
			name:            "gzip",
			contentEncoding: "gzip",
			encode: func(t *testing.T, in []byte) []byte {
				t.Helper()
				var b bytes.Buffer
				zw := gzip.NewWriter(&b)
				if _, err := zw.Write(in); err != nil {
					t.Fatal(err)
				}
				if err := zw.Close(); err != nil {
					t.Fatal(err)
				}
				return b.Bytes()
			},
		},
		{
			name:            "brotli",
			contentEncoding: "br",
			encode: func(t *testing.T, in []byte) []byte {
				t.Helper()
				var b bytes.Buffer
				zw := brotli.NewWriter(&b)
				if _, err := zw.Write(in); err != nil {
					t.Fatal(err)
				}
				if err := zw.Close(); err != nil {
					t.Fatal(err)
				}
				return b.Bytes()
			},
		},
		{
			name:            "deflate",
			contentEncoding: "deflate",
			encode: func(t *testing.T, in []byte) []byte {
				t.Helper()
				var b bytes.Buffer
				zw := zlib.NewWriter(&b)
				if _, err := zw.Write(in); err != nil {
					t.Fatal(err)
				}
				if err := zw.Close(); err != nil {
					t.Fatal(err)
				}
				return b.Bytes()
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			httpClient.Transport = roundTripFunc(func(req *http.Request) *http.Response {
				respRecorder := httptest.NewRecorder()
				respRecorder.Header().Set("Content-Type", "image/svg+xml")
				respRecorder.Header().Set("Content-Encoding", tc.contentEncoding)
				respRecorder.WriteHeader(http.StatusOK)
				_, _ = respRecorder.Write(tc.encode(t, rawSVG))
				return respRecorder.Result()
			})

			out, err := bundleRemoteForTest(ctx, simplelog.FromLibLog(ctx), sampleSVG, false)
			if err != nil {
				t.Fatal(err)
			}
			match := imageRegex.FindSubmatch(out)
			if len(match) != 2 {
				t.Fatalf("expected bundled image href, got %s", out)
			}
			const prefix = `data:image/svg+xml;base64,`
			href := string(match[1])
			if !strings.HasPrefix(href, prefix) {
				t.Fatalf("expected normalized SVG data URI, got %s", href)
			}
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(href, prefix))
			if err != nil {
				t.Fatal(err)
			}
			tassert.Equal(t, rawSVG, decoded)
		})
	}
}

func TestInlineRemoteContentTypeIsSafeAndCanonical(t *testing.T) {
	ctx := log.With(context.Background(), testlog.New(t))
	originalTransport := httpClient.Transport
	t.Cleanup(func() {
		httpClient.Transport = originalTransport
	})
	imageURL := "https://example.com/asset"
	sampleSVG := []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg"><image href="%s" /></svg>`, imageURL))
	rawSVG := []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)

	for _, tc := range []struct {
		name        string
		contentType string
		body        []byte
		wantType    string
	}{
		{
			name:        "malicious quoted header",
			contentType: `image/svg+xml" onerror="alert(1)" data-x="`,
			body:        rawSVG,
			wantType:    "image/svg+xml",
		},
		{
			name:        "parameterized SVG",
			contentType: "image/svg+xml; charset=utf-8",
			body:        rawSVG,
			wantType:    "image/svg+xml",
		},
		{
			name:        "parameterized PNG alias",
			contentType: "image/x-png; charset=binary",
			body:        testPNGFile,
			wantType:    "image/png",
		},
		{
			name:        "XML-sensitive subtype",
			contentType: "image/x&y",
			body:        testPNGFile,
			wantType:    "image/png",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			httpClient.Transport = roundTripFunc(func(req *http.Request) *http.Response {
				if req.URL.String() != imageURL {
					t.Fatalf("unexpected URL %s", req.URL)
				}
				respRecorder := httptest.NewRecorder()
				respRecorder.Header().Set("Content-Type", tc.contentType)
				respRecorder.WriteHeader(http.StatusOK)
				_, _ = respRecorder.Write(tc.body)
				return respRecorder.Result()
			})

			out, err := bundleRemoteForTest(ctx, simplelog.FromLibLog(ctx), sampleSVG, false)
			if err != nil {
				t.Fatal(err)
			}
			wantHref := "data:" + tc.wantType + ";base64," + base64.StdEncoding.EncodeToString(tc.body)
			assertBundledImageHref(t, out, wantHref)
		})
	}
}

func assertBundledImageHref(t *testing.T, source []byte, wantHref string) {
	t.Helper()

	decoder := xml.NewDecoder(bytes.NewReader(source))
	decoder.Strict = true
	found := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("bundled SVG is not valid XML: %v\n%s", err, source)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "image" {
			continue
		}
		for _, attr := range start.Attr {
			if strings.HasPrefix(strings.ToLower(attr.Name.Local), "on") {
				t.Fatalf("bundled image contains event handler %q: %s", attr.Name.Local, source)
			}
			if attr.Name.Local == "href" && attr.Value == wantHref {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("bundled SVG does not contain href %q: %s", wantHref, source)
	}
}

func TestDeprecatedWrapperCacheIsDocumentScoped(t *testing.T) {
	ctx := log.With(context.Background(), testlog.New(t))
	url1 := "https://icons.terrastruct.com/essentials/004-picture.svg"
	url2 := "https://icons.terrastruct.com/essentials/004-picture.svg"

	sampleSVG := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<svg
id="d2-svg"
style="background: white;"
xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"
width="328" height="587" viewBox="-100 -131 328 587"><style type="text/css">
<![CDATA[
.shape {
  shape-rendering: geometricPrecision;
  stroke-linejoin: round;
}
.connection {
  stroke-linecap: round;
  stroke-linejoin: round;
}

]]>
</style><g id="a"><g class="shape" ><image href="%s" x="0" y="0" width="128" height="128" style="fill:#FFFFFF;stroke:#0D32B2;opacity:1.000000;stroke-width:2;" /></g><text class="text-bold" x="64.000000" y="-15.000000" style="text-anchor:middle;font-size:16px;fill:#0A0F25">a</text></g><g id="b"><g class="shape" ><image href="%s" x="0" y="228" width="128" height="128" style="fill:#FFFFFF;stroke:#0D32B2;opacity:1.000000;stroke-width:2;" /></g><text class="text-bold" x="64.000000" y="213.000000" style="text-anchor:middle;font-size:16px;fill:#0A0F25">b</text></g><g id="(a -&gt; b)[0]"><marker id="mk-3990223579" markerWidth="10.000000" markerHeight="12.000000" refX="7.000000" refY="6.000000" viewBox="0.000000 0.000000 10.000000 12.000000" orient="auto" markerUnits="userSpaceOnUse"> <polygon class="connection" fill="#0D32B2" stroke-width="2" points="0.000000,0.000000 10.000000,6.000000 0.000000,12.000000" /> </marker><path d="M 64.000000 130.000000 C 64.000000 168.000000 64.000000 188.000000 64.000000 224.000000" class="connection" style="fill:none;stroke:#0D32B2;opacity:1.000000;stroke-width:2;" marker-end="url(#mk-3990223579)" /></g><style type="text/css"><![CDATA[
.text-bold {
	font-family: "font-bold";
}
@font-face {
	font-family: font-bold;
	src: url("REMOVED");
}]]></style></svg>
`, url1, url2)

	count := 0

	httpClient.Transport = roundTripFunc(func(req *http.Request) *http.Response {
		count++
		respRecorder := httptest.NewRecorder()
		respRecorder.WriteString(`<?xml version=\"1.0\" encoding=\"iso-8859-1\"?>\r\n<!-- Generator: Adobe Illustrator 19.0.0, SVG Export Plug-In . SVG Version: 6.00 Build 0)  -->\r\n<svg version=\"1.1\" id=\"Capa_1\" xmlns=\"http://www.w3.org/2000/svg\" xmlns:xlink=\"http://www.w3.org/1999/xlink\" x=\"0px\" y=\"0px\"\r\n\t viewBox=\"0 0 58 58\" style=\"enable-background:new 0 0 58 58;\" xml:space=\"preserve\">\r\n<rect x=\"1\" y=\"7\" style=\"fill:#C3E1ED;stroke:#E7ECED;stroke-width:2;stroke-miterlimit:10;\" width=\"56\" height=\"44\"/>\r\n<circle style=\"fill:#ED8A19;\" cx=\"16\" cy=\"17.569\" r=\"6.569\"/>\r\n<polygon style=\"fill:#1A9172;\" points=\"56,36.111 55,35 43,24 32.5,35.5 37.983,40.983 42,45 56,45 \"/>\r\n<polygon style=\"fill:#1A9172;\" points=\"2,49 26,49 21.983,44.983 11.017,34.017 2,41.956 \"/>\r\n<rect x=\"2\" y=\"45\" style=\"fill:#6B5B4B;\" width=\"54\" height=\"5\"/>\r\n<polygon style=\"fill:#25AE88;\" points=\"37.983,40.983 27.017,30.017 10,45 42,45 \"/>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n<g>\r\n</g>\r\n</svg>`)
		respRecorder.WriteHeader(200)
		respRecorder.Body.Reset()
		respRecorder.WriteString(canonicalTestSVG)
		return respRecorder.Result()
	})

	l := simplelog.FromLibLog(ctx)
	// Deprecated wrappers retain the cacheImages parameter for source
	// compatibility, but cache state cannot escape one document invocation.
	_, err := bundleRemoteForTest(ctx, l, []byte(sampleSVG), true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = bundleRemoteForTest(ctx, l, []byte(sampleSVG), true)
	if err != nil {
		t.Fatal(err)
	}
	tassert.Equal(t, 2, count)

	// With cache disabled, it refetches
	count = 0
	_, err = bundleRemoteForTest(ctx, l, []byte(sampleSVG), false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = bundleRemoteForTest(ctx, l, []byte(sampleSVG), false)
	if err != nil {
		t.Fatal(err)
	}
	tassert.Equal(t, 2, count)
}
