package d2cli

import (
	"bytes"
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/d2lang/util-go/xmain"
	"github.com/d2lang/util-go/xos"
)

func TestOutputFormat(t *testing.T) {
	type testCase struct {
		stdoutFormatFlag          string
		outputPath                string
		extension                 exportExtension
		supportsDarkTheme         bool
		supportsAnimation         bool
		requiresAnimationInterval bool
	}
	testCases := []testCase{
		{
			outputPath:                "/out.svg",
			extension:                 SVG,
			supportsDarkTheme:         true,
			supportsAnimation:         true,
			requiresAnimationInterval: false,
		},
		{
			// assumes SVG by default
			outputPath:                "/out",
			extension:                 SVG,
			supportsDarkTheme:         true,
			supportsAnimation:         true,
			requiresAnimationInterval: false,
		},
		{
			outputPath:                "-",
			extension:                 SVG,
			supportsDarkTheme:         true,
			supportsAnimation:         true,
			requiresAnimationInterval: false,
		},
		{
			stdoutFormatFlag:          "png",
			outputPath:                "-",
			extension:                 PNG,
			supportsDarkTheme:         false,
			supportsAnimation:         false,
			requiresAnimationInterval: false,
		},
		{
			outputPath:                "/out.png",
			extension:                 PNG,
			supportsDarkTheme:         false,
			supportsAnimation:         false,
			requiresAnimationInterval: false,
		},
		{
			outputPath:                "/out.pptx",
			extension:                 PPTX,
			supportsDarkTheme:         false,
			supportsAnimation:         false,
			requiresAnimationInterval: false,
		},
		{
			outputPath:                "/out.pdf",
			extension:                 PDF,
			supportsDarkTheme:         false,
			supportsAnimation:         false,
			requiresAnimationInterval: false,
		},
		{
			outputPath:                "/out.gif",
			extension:                 GIF,
			supportsDarkTheme:         false,
			supportsAnimation:         true,
			requiresAnimationInterval: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.outputPath, func(t *testing.T) {
			extension, err := getOutputFormat(&tc.stdoutFormatFlag, tc.outputPath)
			assert.NoError(t, err)
			assert.Equal(t, tc.extension, extension)
			assert.Equal(t, tc.supportsAnimation, extension.supportsAnimation())
			assert.Equal(t, tc.supportsDarkTheme, extension.supportsDarkTheme())
		})
	}
}

func TestUnsupportedStdoutFormatDoesNotWrite(t *testing.T) {
	for _, format := range []string{"html", "HTML", "unknown"} {
		t.Run(format, func(t *testing.T) {
			data, err := imageCLI(t, t.TempDir(), "a -> b", "--stdout-format="+format, "-")
			if err == nil || !strings.Contains(err.Error(), "is not a supported format") {
				t.Fatalf("unsupported format %q: got %v", format, err)
			}
			if len(data) != 0 {
				t.Fatal("unsupported format wrote output")
			}
		})
	}
}

func TestUnknownOutputExtensionUsesSVG(t *testing.T) {
	for _, name := range []string{"out.html", "out.unknown"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := imageCLI(t, dir, "a -> b", name); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			var document struct{ XMLName xml.Name }
			if err := xml.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			if document.XMLName.Local != "svg" || document.XMLName.Space != "http://www.w3.org/2000/svg" {
				t.Fatalf("unknown extension no longer defaults to SVG: %v", document.XMLName)
			}
		})
	}
}

func imageCLI(t *testing.T, dir, source string, args ...string) ([]byte, error) {
	t.Helper()
	return imageCLIWithEnv(t, dir, source, nil, args...)
}

func imageCLIWithEnv(t *testing.T, dir, source string, env []string, args ...string) ([]byte, error) {
	t.Helper()
	input := filepath.Join(dir, "input.d2")
	if err := os.WriteFile(input, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	state := &xmain.TestState{
		Run:    Run,
		Args:   append([]string{"d2", input}, args...),
		PWD:    dir,
		Stdout: &stdout,
		Env:    xos.NewEnv(append([]string{"PATH="}, env...)),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	state.Start(t, ctx)
	defer state.Cleanup(t)
	err := state.Wait(ctx)
	return stdout.Bytes(), err
}

func TestRefuseToOverwriteSource(t *testing.T) {
	for _, alias := range []string{"same path", "symlink", "hard link"} {
		t.Run(alias, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "source.d2")
			output := filepath.Join(dir, "result.svg")
			const source = "a -> b\n"
			if err := os.WriteFile(input, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			switch alias {
			case "same path":
				output = input
			case "symlink":
				if err := os.Symlink(input, output); err != nil {
					t.Skip(err)
				}
			case "hard link":
				if err := os.Link(input, output); err != nil {
					t.Skip(err)
				}
			}
			err := runBoardOutputCLIResult(t, dir, input, output)
			if err == nil || !strings.Contains(err.Error(), "same file") {
				t.Fatalf("got %v", err)
			}
			got, err := os.ReadFile(input)
			if err != nil || string(got) != source {
				t.Fatalf("source = %q, %v", got, err)
			}
		})
	}
}

func TestTextExportHonorsSourceLayout(t *testing.T) {
	const source = "vars.d2-config.layout-engine: tala\na: {top: 60; left: 30}\na -> b"
	got, err := imageCLI(t, t.TempDir(), source, "--stdout-format=txt", "-")
	if err != nil {
		t.Fatal(err)
	}
	want, err := imageCLI(t, t.TempDir(), source, "--layout=tala", "--stdout-format=txt", "-")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("source TALA differs from explicit TALA: %v", err)
	}
	_, err = imageCLI(t, t.TempDir(), source, "--layout=DAGRE", "--stdout-format=txt", "-")
	if err == nil || !strings.Contains(err.Error(), `layout engine "elk"`) {
		t.Fatalf("explicit Dagre should select ELK for text: %v", err)
	}
}
