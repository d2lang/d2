package d2cli

import (
	"fmt"
	"path/filepath"
	"strings"
)

type exportExtension string

const GIF exportExtension = ".gif"
const MP4 exportExtension = ".mp4"
const WEBM exportExtension = ".webm"
const PNG exportExtension = ".png"
const PPTX exportExtension = ".pptx"
const PDF exportExtension = ".pdf"
const SVG exportExtension = ".svg"
const TXT exportExtension = ".txt"

var SUPPORTED_EXTENSIONS = []exportExtension{SVG, PNG, PDF, PPTX, GIF, MP4, WEBM, TXT}

var STDOUT_FORMAT_MAP = map[string]exportExtension{
	"png":   PNG,
	"svg":   SVG,
	"ascii": TXT,
	"txt":   TXT,
	"pdf":   PDF,
	"pptx":  PPTX,
	"gif":   GIF,
	"mp4":   MP4,
	"webm":  WEBM,
}

var SUPPORTED_STDOUT_FORMATS = []string{"png", "svg", "ascii", "txt", "pdf", "pptx", "gif", "mp4", "webm"}

func getOutputFormat(stdoutFormatFlag *string, outputPath string) (exportExtension, error) {
	if stdoutFormatFlag != nil && *stdoutFormatFlag != "" {
		format := strings.ToLower(*stdoutFormatFlag)
		if ext, ok := STDOUT_FORMAT_MAP[format]; ok {
			return ext, nil
		}
		return "", fmt.Errorf("%s is not a supported format. Supported formats are: %s", *stdoutFormatFlag, SUPPORTED_STDOUT_FORMATS)
	}
	return getExportExtension(outputPath), nil
}

func getExportExtension(outputPath string) exportExtension {
	ext := filepath.Ext(outputPath)
	for _, kext := range SUPPORTED_EXTENSIONS {
		if kext == exportExtension(ext) {
			return exportExtension(ext)
		}
	}
	// default is svg
	return exportExtension(SVG)
}

func (ex exportExtension) supportsAnimation() bool {
	return ex == SVG || ex == GIF || ex.isVideo()
}

func (ex exportExtension) isVideo() bool {
	return ex == MP4 || ex == WEBM
}

func (ex exportExtension) supportsDarkTheme() bool {
	return ex == SVG
}
