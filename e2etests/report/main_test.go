package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T, root, path, content string) string {
	t.Helper()
	path = filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoverSnapshotsAndDelta(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "stable/example/dagre/sketch.exp.svg", "same")
	fixture(t, root, "stable/example/dagre/sketch.got.svg", "same")
	fixture(t, root, "stable/changed/dagre/sketch.exp.svg", "old")
	fixture(t, root, "stable/changed/dagre/sketch.got.svg", "new")
	fixture(t, root, "stable/new/elk/sketch.got.svg", "new snapshot")
	fixture(t, root, "regression/example/elk/sketch.exp.svg", "other set")
	fixture(t, root, "stable/example/elk/alternate.exp.svg", "legacy svg")
	fixture(t, root, "stable/example/dagre/board.exp.json", "{}")
	fixture(t, root, "stable/example/dagre/raster.exp.png", "PNG snapshot")
	fixture(t, root, "stable/example/dagre/sketch..exp.png", "empty suffix")
	fixture(t, root, "stable/example/dagre/sketch..exp.svg", "empty suffix")
	cases := []struct {
		name  string
		opts  discoveryOptions
		names []string
	}{
		{"all", discoveryOptions{}, []string{
			"regression/example/elk/sketch.svg", "stable/changed/dagre/sketch.svg",
			"stable/example/dagre/sketch.svg", "stable/example/elk/alternate.svg", "stable/new/elk/sketch.svg",
		}},
		{"delta", discoveryOptions{Delta: true}, []string{"stable/changed/dagre/sketch.svg", "stable/new/elk/sketch.svg"}},
		{"set and case", discoveryOptions{TestSet: "^regression$", TestCase: "^example$"}, []string{"regression/example/elk/sketch.svg"}},
		{"no match", discoveryOptions{TestCase: "absent"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := discoverTests(root, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			ids := map[string]bool{}
			for _, item := range got {
				names = append(names, item.Name)
				if ids[item.ID] || item.ID == "" {
					t.Errorf("nonunique ID %q", item.ID)
				}
				ids[item.ID] = true
				if item.Name == "stable/new/elk/sketch.svg" && (!item.MissingExpected || item.ExpImage != "" || item.GotLabel != "Got") {
					t.Errorf("missing baseline not represented: %+v", item)
				}
				if item.Name == "stable/changed/dagre/sketch.svg" && (item.ExpImage == "" || item.GotLabel != "Got") {
					t.Errorf("change not paired: %+v", item)
				}
				if item.Name == "stable/example/dagre/sketch.svg" && (item.ExpImage != "" || item.GotLabel != "Expected") {
					t.Errorf("equal got not ignored: %+v", item)
				}
			}
			if !reflect.DeepEqual(names, tc.names) {
				t.Fatalf("got %v; want %v", names, tc.names)
			}
		})
	}
}

func TestDiscoverSnapshotErrors(t *testing.T) {
	for _, opts := range []discoveryOptions{{TestSet: "["}, {TestCase: "["}} {
		if _, err := discoverTests(t.TempDir(), opts); err == nil {
			t.Errorf("accepted %+v", opts)
		}
	}
	if _, err := discoverTests(filepath.Join(t.TempDir(), "absent"), discoveryOptions{}); err == nil {
		t.Fatal("ignored missing snapshot tree")
	}
}

func TestReportRelativeLinksAndEscaping(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "testdata")
	fixture(t, data, "stable/space # & <case>/elk/sketch.got.svg", "new")
	items, err := discoverTests(data, discoveryOptions{Delta: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items", len(items))
	}
	original := items[0].GotImage
	path := filepath.Join(root, "review", "nested", "report.html")
	if err := writeReport(path, items); err != nil {
		t.Fatal(err)
	}
	if items[0].GotImage != original {
		t.Fatal("writeReport mutated discovery results")
	}
	output, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(output)
	for _, want := range []string{"Missing expected snapshot", "space # &amp; &lt;case&gt;", "../../testdata/stable/space%20%23%20&amp;%20%3Ccase%3E/elk/sketch.got.svg", `id="test-1"`, `alt="Got `} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in %s", want, html)
		}
	}
	if strings.Contains(html, root) || strings.Contains(html, "ZgotmplZ") {
		t.Fatal("absolute or unsafe link in report")
	}
	if err := writeReport(path, nil); err != nil {
		t.Fatal(err)
	}
	output, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "No snapshots match") || strings.Contains(string(output), "Missing expected") {
		t.Fatal("empty report left stale results")
	}
}

func TestDiscoverChangedSVG(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "stable/diagram/dagre/sketch.exp.svg", "old SVG")
	fixture(t, root, "stable/diagram/dagre/sketch.got.svg", "new SVG")
	items, err := discoverTests(root, discoveryOptions{Delta: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ExpImage == "" || items[0].GotLabel != "Got" || items[0].MissingExpected {
		t.Fatalf("changed SVG should have old/new pair: %+v", items)
	}
}

func TestDiscoverLayoutFreeAndNestedCases(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "asciitxtar/focused/sketch.exp.svg", "ascii svg")
	fixture(t, root, "asciitxtar/other/sketch.exp.svg", "other")
	fixture(t, root, "asciitxtar/nested/focused/sketch.exp.svg", "nested ascii")
	fixture(t, root, "asciitxtar/nested/elk/sketch.exp.svg", "case named elk")
	fixture(t, root, "stable/group/focused/dagre/sketch.exp.svg", "nested svg")
	fixture(t, root, "stable/group/focused/elk/sketch.exp.svg", "nested elk")
	fixture(t, root, "stable/group/focused/tala/sketch.exp.svg", "nested tala")
	fixture(t, root, "stable/group/other/elk/sketch.exp.svg", "other nested case")
	for _, tc := range []struct {
		set, testcase string
		names         []string
	}{
		{"^asciitxtar$", "^focused$", []string{"asciitxtar/focused/sketch.svg"}},
		{"^asciitxtar$", "^nested/focused$", []string{"asciitxtar/nested/focused/sketch.svg"}},
		{"^asciitxtar$", "^nested/elk$", []string{"asciitxtar/nested/elk/sketch.svg"}},
		{"^stable$", "^group/focused$", []string{"stable/group/focused/dagre/sketch.svg", "stable/group/focused/elk/sketch.svg", "stable/group/focused/tala/sketch.svg"}},
	} {
		t.Run(tc.set+tc.testcase, func(t *testing.T) {
			items, err := discoverTests(root, discoveryOptions{TestSet: tc.set, TestCase: tc.testcase})
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, item := range items {
				names = append(names, item.Name)
			}
			if !reflect.DeepEqual(names, tc.names) {
				t.Fatalf("got %v; want %v", names, tc.names)
			}
		})
	}
}
