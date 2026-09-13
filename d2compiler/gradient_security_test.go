package d2compiler_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
)

func TestCompileRejectsGradientStopMarkup(t *testing.T) {
	t.Parallel()

	for _, gradientType := range []string{"linear", "radial"} {
		gradientType := gradientType
		t.Run(gradientType, func(t *testing.T) {
			t.Parallel()
			dsl := `x.style.fill: '` + gradientType + `-gradient(red 0%"/><script>document.documentElement.dataset.pwned=1</script><!--, blue --><stop/>)'`
			_, _, err := d2compiler.Compile("gradient-security.d2", strings.NewReader(dsl), nil)
			if err == nil || !strings.Contains(err.Error(), `expected "fill" to be a valid named color`) {
				t.Fatalf("Compile() error = %v, want invalid-color diagnostic", err)
			}
		})
	}
}
