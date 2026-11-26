package nodeshape

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

// equivalenceDigest is the SHA-256 of dumpAllShapes, recorded from the
// implementation with one Go type per shape. Every refactor of this package
// must keep it. Set NODESHAPE_DUMP=path to write the dump for a diff.
const equivalenceDigest = "c737bcab544b1cdd3e204dbc39e9b0d20870a695860d4001f6bd4ad2fc5661dc"

func TestEquivalenceDigest(t *testing.T) {
	var b strings.Builder
	dumpAllShapes(&b)
	if path := os.Getenv("NODESHAPE_DUMP"); path != "" {
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	if got := hex.EncodeToString(sum[:]); got != equivalenceDigest {
		t.Fatalf("shape policy dump changed: digest %s, want %s", got, equivalenceDigest)
	}
}

var equivalenceTypeNames = []string{
	"", shape.SQUARE_TYPE, shape.REAL_SQUARE_TYPE, shape.PARALLELOGRAM_TYPE, shape.DOCUMENT_TYPE,
	shape.CYLINDER_TYPE, shape.QUEUE_TYPE, shape.PAGE_TYPE, shape.PACKAGE_TYPE, shape.STEP_TYPE,
	shape.CALLOUT_TYPE, shape.STORED_DATA_TYPE, shape.PERSON_TYPE, shape.C4_PERSON_TYPE,
	shape.DIAMOND_TYPE, shape.OVAL_TYPE, shape.CIRCLE_TYPE, shape.HEXAGON_TYPE, shape.CLOUD_TYPE,
	shape.TABLE_TYPE, shape.CLASS_TYPE, shape.TEXT_TYPE, shape.CODE_TYPE, shape.IMAGE_TYPE,
	// Unsupported names.
	"unknown", "square", "Rectangle", " ", "Square ",
}

var equivalenceBoxes = []geo.Box{
	{TopLeft: geo.NewPoint(0, 0), Width: 100, Height: 100},
	{TopLeft: geo.NewPoint(10, 20), Width: 300, Height: 60},
	{TopLeft: geo.NewPoint(-5, 7.5), Width: 40, Height: 200},
	{TopLeft: geo.NewPoint(1, 1), Width: 3, Height: 2},
	{TopLeft: geo.NewPoint(0, 0), Width: 0, Height: 0},
	{TopLeft: geo.NewPoint(1.1, 2.2), Width: 33.3, Height: 17.77},
	{TopLeft: geo.NewPoint(0, 0), Width: 52, Height: 47.5},
}

func dumpAllShapes(w io.Writer) {
	for kind := Kind(0); kind < 30; kind++ {
		fmt.Fprintf(w, "Kind(%d).String()=%q\n", kind, kind.String())
	}
	fmt.Fprintf(w, "Kind(255).String()=%q\n", Kind(255).String())
	for _, name := range equivalenceTypeNames {
		for bi := range equivalenceBoxes {
			columns := []int{-1, 3} // SetNumColumns is a no-op on non-tables.
			if name == shape.TABLE_TYPE {
				columns = []int{-1, 0, 1, 2, 3, 4, 5, 6}
			}
			for _, numColumns := range columns {
				fmt.Fprintf(w, "== New(%q) box %d columns %d\n", name, bi, numColumns)
				dumpNew(w, name, equivalenceBoxes[bi], numColumns)
			}
		}
	}
}

func dumpNew(w io.Writer, name string, b geo.Box, numColumns int) {
	box := b
	box.TopLeft = geo.NewPoint(b.TopLeft.X, b.TopLeft.Y)
	s, kind, ok := New(name, &box)
	fmt.Fprintf(w, "ok=%v kind=%d(%s) nil=%v\n", ok, kind, kind, s == nil)
	if !ok {
		return
	}
	if numColumns >= 0 {
		SetNumColumns(s, numColumns)
	}
	fmt.Fprintf(w, "NumColumns=%d\n", NumColumns(s))
	_, isPathProvider := any(s).(shape.PathCommandProvider)
	fmt.Fprintf(w, "PathCommandProvider=%v GetBox==box:%v\n", isPathProvider, s.GetBox() == &box)
	dumpD2Shape(w, s)
	tableIndex := func(o geo.Orientation, column int) (int, bool) { return TablePortIndex(s, o, column) }
	tablePort := func(o geo.Orientation, column int) (geo.Point, bool) { return TableColumnPortValue(s, o, column) }
	dumpPolicy(w, s)
	dumpTablePorts(w, tableIndex, tablePort)

	// The shape follows its box pointer, as nodes move and resize.
	box.TopLeft.X += 7
	box.TopLeft.Y -= 3
	box.Width = box.Width*1.5 + 1
	box.Height = box.Height/2 + 0.25
	fmt.Fprintf(w, "-- after box change\n")
	dumpD2Shape(w, s)
	dumpPolicy(w, s)
	dumpTablePorts(w, tableIndex, tablePort)

	s.SetInnerBoxAspectRatio(1.5)
	fmt.Fprintf(w, "-- after SetInnerBoxAspectRatio(1.5)\n")
	dumpD2Shape(w, s)
}

func dumpD2Shape(w io.Writer, s shape.Shape) {
	for _, name := range equivalenceTypeNames {
		fmt.Fprintf(w, "Is(%q)=%v ", name, s.Is(name))
	}
	fmt.Fprintf(w, "\nGetType=%q AspectRatio1=%v IsRectangular=%v\n", s.GetType(), s.AspectRatio1(), s.IsRectangular())
	fmt.Fprintf(w, "GetBox=%s\nGetInnerBox=%s\n", dumpValue(s.GetBox()), dumpValue(s.GetInnerBox()))
	for _, wh := range [][2]float64{{10, 20}, {200, 50}, {0, 0}} {
		fmt.Fprintf(w, "GetInnerBoxForContent(%v,%v)=%s\n", wh[0], wh[1], dumpValue(s.GetInnerBoxForContent(wh[0], wh[1])))
	}
	fmt.Fprintf(w, "GetInsidePlacement=%s\n", dumpValue(s.GetInsidePlacement(10, 20, 5, 6)))
	dw, dh := s.GetDimensionsToFit(30, 40, 5, 6)
	px, py := s.GetDefaultPadding()
	fmt.Fprintf(w, "GetDimensionsToFit=%s,%s GetDefaultPadding=%s,%s\n", fl(dw), fl(dh), fl(px), fl(py))
	fmt.Fprintf(w, "Perimeter=%s\n", dumpValue(s.Perimeter()))
	fmt.Fprintf(w, "GetSVGPathData=%q\n", s.GetSVGPathData())
}

var equivalenceOrientations = []geo.Orientation{
	-1, geo.TopLeft, geo.TopRight, geo.BottomLeft, geo.BottomRight,
	geo.Top, geo.Right, geo.Bottom, geo.Left, geo.NONE, geo.NONE + 1,
}

// policyShape lists the methods TALA adds to D2's shapes.
type policyShape interface {
	SnapPointPercentages() [][]*geo.RelativePoint
	MirroredPortIndices() map[int]int
	CenterPortIndices() []int
	PortIndices(orientation geo.Orientation) []int
	CenterPortIndex(orientation geo.Orientation) int
	LabelPositionPreferences(tier LabelTier) LabelSet
}

func dumpPolicy(w io.Writer, s policyShape) {
	a, b := s.SnapPointPercentages(), s.SnapPointPercentages()
	fmt.Fprintf(w, "SnapPointPercentages=%s %s\n", dumpValue(a), sliceInfo(a, b))
	for i := range a {
		fmt.Fprintf(w, " group %d %s", i, sliceInfo(a[i], b[i]))
		for j := range a[i] {
			fmt.Fprintf(w, " pointShared=%v", a[i][j] == b[i][j])
		}
		fmt.Fprintln(w)
	}
	m1, m2 := s.MirroredPortIndices(), s.MirroredPortIndices()
	fmt.Fprintf(w, "MirroredPortIndices=%s nil=%v shared=%v\n", dumpValue(m1), m1 == nil,
		m1 != nil && reflect.ValueOf(m1).Pointer() == reflect.ValueOf(m2).Pointer())
	c1, c2 := s.CenterPortIndices(), s.CenterPortIndices()
	fmt.Fprintf(w, "CenterPortIndices=%#v %s\n", c1, sliceInfo(c1, c2))
	for _, o := range equivalenceOrientations {
		p1, p2 := s.PortIndices(o), s.PortIndices(o)
		fmt.Fprintf(w, "PortIndices(%d)=%#v %s CenterPortIndex=%d\n", o, p1, sliceInfo(p1, p2), s.CenterPortIndex(o))
	}
	for tier := LabelTier(-1); tier <= Bad+1; tier++ {
		fmt.Fprintf(w, "LabelPositionPreferences(%d)=%#x\n", tier, uint64(s.LabelPositionPreferences(tier)))
	}
}

func dumpTablePorts(w io.Writer, tableIndex func(geo.Orientation, int) (int, bool), tablePort func(geo.Orientation, int) (geo.Point, bool)) {
	for _, o := range equivalenceOrientations {
		for column := -1; column <= 7; column++ {
			index, ok, msg := tablePortIndex(tableIndex, o, column)
			port, portOK, portMsg := tableColumnPortValue(tablePort, o, column)
			fmt.Fprintf(w, "Table(%d,%d) index=%d,%v%s port=%s,%v%s\n", o, column, index, ok, msg, dumpValue(port), portOK, portMsg)
		}
	}
}

func tablePortIndex(f func(geo.Orientation, int) (int, bool), o geo.Orientation, column int) (index int, ok bool, msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf(" panic(%v)", r)
		}
	}()
	index, ok = f(o, column)
	return index, ok, ""
}

func tableColumnPortValue(f func(geo.Orientation, int) (geo.Point, bool), o geo.Orientation, column int) (port geo.Point, ok bool, msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf(" panic(%v)", r)
		}
	}()
	port, ok = f(o, column)
	return port, ok, ""
}

// sliceInfo reports a slice's length, nil-ness and whether two results share
// their backing array. Capacity is left out: callers never append to these
// slices, and a fresh slice's spare capacity is not part of its value.
func sliceInfo[T any](a, b []T) string {
	return fmt.Sprintf("len=%d nil=%v shared=%v", len(a), a == nil, len(a) > 0 && &a[0] == &b[0])
}

func fl(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// dumpValue prints v deterministically, following pointers instead of
// printing their addresses.
func dumpValue(v any) string {
	var b strings.Builder
	dumpReflect(&b, reflect.ValueOf(v))
	return b.String()
}

func dumpReflect(b *strings.Builder, v reflect.Value) {
	if !v.IsValid() {
		b.WriteString("invalid")
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			b.WriteString("nil")
			return
		}
		b.WriteString("&")
		dumpReflect(b, v.Elem())
	case reflect.Interface:
		if v.IsNil() {
			b.WriteString("nil")
			return
		}
		b.WriteString(v.Elem().Type().String())
		dumpReflect(b, v.Elem())
	case reflect.Struct:
		b.WriteString("{")
		for i := 0; i < v.NumField(); i++ {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(v.Type().Field(i).Name + ":")
			dumpReflect(b, v.Field(i))
		}
		b.WriteString("}")
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			b.WriteString("nil")
			return
		}
		b.WriteString("[")
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				b.WriteString(" ")
			}
			dumpReflect(b, v.Index(i))
		}
		b.WriteString("]")
	case reflect.Map:
		if v.IsNil() {
			b.WriteString("nil")
			return
		}
		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			var kb, vb strings.Builder
			dumpReflect(&kb, k)
			dumpReflect(&vb, v.MapIndex(k))
			keys = append(keys, kb.String()+":"+vb.String())
		}
		sort.Strings(keys)
		b.WriteString("map[" + strings.Join(keys, " ") + "]")
	case reflect.Float32, reflect.Float64:
		b.WriteString(fl(v.Float()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		b.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		b.WriteString(strconv.FormatUint(v.Uint(), 10))
	case reflect.Bool:
		b.WriteString(strconv.FormatBool(v.Bool()))
	case reflect.String:
		b.WriteString(strconv.Quote(v.String()))
	default:
		fmt.Fprintf(b, "<%s>", v.Kind())
	}
}
