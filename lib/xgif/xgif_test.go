package xgif

import (
	_ "embed"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

//go:embed test_input1.png
var test_input1 []byte

//go:embed test_input2.png
var test_input2 []byte

//go:embed test_output.gif
var test_output []byte

func TestPngToGif(t *testing.T) {
	boards := [][]byte{test_input1, test_input2}
	interval := 1_000
	gifBytes, err := AnimatePNGs(boards, interval)
	assert.NoError(t, err)

	// use this to update the test output
	if false {
		f, err := os.Create("test_output_2.gif")
		assert.NoError(t, err)
		defer f.Close()
		f.Write(gifBytes)
	}

	assert.Equal(t, test_output, gifBytes)
}

func TestFrameCountForDuration(t *testing.T) {
	assert.Equal(t, 1, frameCountForDuration(10))
	assert.Equal(t, 1, frameCountForDuration(999/fps))
	assert.Equal(t, 30, frameCountForDuration(1000))
	assert.Equal(t, 31, frameCountForDuration(1001))
}

func TestFrameDelayForPNGs(t *testing.T) {
	assert.Equal(t, 1, frameDelayForPNGs(10, 1))
	assert.Equal(t, 1, frameDelayForPNGs(10, 4))
	assert.Equal(t, 3, frameDelayForPNGs(1000, frameCountForDuration(1000)))
}
