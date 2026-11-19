package d2cli

import "fmt"

func isometricExportExtension(ext exportExtension) (exportExtension, error) {
	switch ext {
	case SVG, isometricSVG:
		return isometricSVG, nil
	case PNG, isometricPNG:
		return isometricPNG, nil
	case GIF, isometricGIF:
		return isometricGIF, nil
	case PDF, isometricPDF:
		return isometricPDF, nil
	case PPTX, isometricPPTX:
		return isometricPPTX, nil
	default:
		return "", fmt.Errorf("--isometric exports SVG, PNG, GIF, PDF or PPTX; use a matching filename or --stdout-format")
	}
}
