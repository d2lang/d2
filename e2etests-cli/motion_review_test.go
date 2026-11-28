package e2etests_cli

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/d2lang/util-go/xmain"
	"github.com/d2lang/util-go/xos"
	"github.com/stretchr/testify/require"
)

func TestMotionReviewCLI(t *testing.T) {
	dir := t.TempDir()
	const film = "vars: {d2-config: {animate-interval: 400ms}}\ncache\nsteps: {hit: {cache.label: 2 ms}; done: {cache.label: Cache}}"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "film.d2"), []byte(film), 0600))
	run := func(env []string, stdin string, args ...string) ([]byte, error) {
		t.Helper()
		var out bytes.Buffer
		state := testMain(dir, xos.NewEnv(env), args...)
		state.Stdin, state.Stdout = strings.NewReader(stdin), &out
		state.Start(t, t.Context())
		defer state.Cleanup(t)
		err := state.Wait(t.Context())
		return out.Bytes(), err
	}

	// Source timing and an explicit override must reach both review commands.
	data, err := run(nil, "", "lint", "--motion", "film.d2")
	var status xmain.ExitError
	require.True(t, errors.As(err, &status))
	require.Equal(t, 1, status.Code)
	require.Contains(t, string(data), "steps.hit")
	require.Contains(t, string(data), "2 ms")
	data, err = run([]string{"D2_ANIMATE_INTERVAL=50"}, "", "lint", "--motion", "--animate-interval=4000", "film.d2")
	require.NoError(t, err)
	require.Empty(t, data)

	data, err = run(nil, "", "storyboard", "film.d2")
	require.NoError(t, err)
	require.Empty(t, data)
	named, err := os.ReadFile(filepath.Join(dir, "film.storyboard.png"))
	require.NoError(t, err)
	_, err = png.Decode(bytes.NewReader(named))
	require.NoError(t, err)
	data, err = run(nil, film, "storyboard", "-", "-")
	require.NoError(t, err)
	require.Equal(t, named, data)
	data, err = run(nil, film, "storyboard", "--animate-interval=2000", "-", "-")
	require.NoError(t, err)
	require.False(t, bytes.Equal(named, data), "the timestamp labels should change")

	// SVG appends numbered tooltip notes only when requested. The
	// storyboard should show the same frame content in all three cases.
	const tooltip = "service: {tooltip: A note below the frame}"
	svgDefault, err := run(nil, tooltip, "storyboard", "-", "-")
	require.NoError(t, err)
	withNotes := "vars.d2-config.force-appendix: true\n" + tooltip
	sourceOn, err := run(nil, withNotes, "storyboard", "-", "-")
	require.NoError(t, err)
	require.False(t, bytes.Equal(svgDefault, sourceOn))
	flagOff, err := run(nil, withNotes, "storyboard", "--force-appendix=false", "-", "-")
	require.NoError(t, err)
	require.Equal(t, svgDefault, flagOff)

	for _, args := range [][]string{
		{"lint", "film.d2"}, {"lint", "--motion", "film.d2", "extra"},
		{"storyboard"}, {"storyboard", "film.d2", "film.d2"},
		{"storyboard", "--scale=2", "film.d2", "-"},
		{"storyboard", "--animate-interval=-1", "film.d2", "-"},
	} {
		data, err = run(nil, "", args...)
		require.Error(t, err, "%v", args)
		require.Empty(t, data)
	}
}
