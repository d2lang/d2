package d2themescatalog

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2themes"
)

var LightCatalog = []d2themes.Theme{
	NeutralDefault,
	NeutralGrey,
	FlagshipTerrastruct,
	CoolClassics,
	MixedBerryBlue,
	GrapeSoda,
	Aubergine,
	ColorblindClear,
	VanillaNitroCola,
	OrangeCreamsicle,
	ShirleyTemple,
	EarthTones,
	EvergladeGreen,
	ButteredToast,
	Terminal,
	TerminalGrayscale,
	Origami,
	C4,
}

var DarkCatalog = []d2themes.Theme{
	DarkMauve,
	DarkFlagshipTerrastruct,
}

func Find(id int64) d2themes.Theme {
	for _, theme := range LightCatalog {
		if theme.ID == id {
			return theme
		}
	}

	for _, theme := range DarkCatalog {
		if theme.ID == id {
			return theme
		}
	}

	return d2themes.Theme{}
}

// Resolve accepts a decimal theme ID or a case-insensitive catalog name.
// Hyphens can stand in for spaces in names such as "Terminal Grayscale".
func Resolve(value string) (d2themes.Theme, error) {
	if id, err := strconv.ParseInt(value, 10, 64); err == nil {
		theme := Find(id)
		if theme.Name == "" {
			return d2themes.Theme{}, fmt.Errorf("%d is not a valid theme ID", id)
		}
		return theme, nil
	}

	name := strings.ReplaceAll(value, "-", " ")
	for _, catalog := range [][]d2themes.Theme{LightCatalog, DarkCatalog} {
		for i := range catalog {
			if strings.EqualFold(catalog[i].Name, name) {
				return catalog[i], nil
			}
		}
	}
	return d2themes.Theme{}, fmt.Errorf("unknown theme %q (run 'd2 themes' to list available themes)", value)
}

func CLIString() string {
	var s strings.Builder

	s.WriteString("Light:\n")
	for _, t := range LightCatalog {
		s.WriteString(fmt.Sprintf("- %s: %d\n", t.Name, t.ID))
	}

	s.WriteString("Dark:\n")
	for _, t := range DarkCatalog {
		s.WriteString(fmt.Sprintf("- %s: %d\n", t.Name, t.ID))
	}

	return s.String()
}
