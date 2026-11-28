package d2cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/d2lang/util-go/go2"
	"github.com/d2lang/util-go/xmain"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2renderers/d2ascii"
	"github.com/d2lang/d2/d2renderers/d2ascii/charset"
	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2renderers/d2svg/appendix"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/internal/d2layoutfeatures"
	"github.com/d2lang/d2/lib/background"
	"github.com/d2lang/d2/lib/imgbundler"
	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/netpolicy"
	"github.com/d2lang/d2/lib/pdf"
	"github.com/d2lang/d2/lib/pptx"
	"github.com/d2lang/d2/lib/simplelog"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/d2/lib/version"
)

func resolveThemeFlag(ms *xmain.State, flag, value string) (*int64, error) {
	// The CLI's integer flags accept base prefixes, while environment variables use decimal.
	base := 10
	if ms.Opts.Flags.Changed(flag) {
		base = 0
	}
	lookup := value
	if id, err := strconv.ParseInt(value, base, 64); err == nil {
		if flag == "dark-theme" && id == -1 {
			return nil, nil
		}
		lookup = strconv.FormatInt(id, 10)
	}
	theme, err := d2themescatalog.Resolve(lookup)
	if err != nil {
		return nil, xmain.UsageErrorf("--%s could not be found. The available options are:\n%s\nYou provided: %s", flag, d2themescatalog.CLIString(), value)
	}
	ms.Log.Debug.Printf("using %s %s (ID: %d)", flag, theme.Name, theme.ID)
	return go2.Pointer(theme.ID), nil
}

func privateNetworkEnvDefault(ms *xmain.State) (bool, error) {
	const key = "D2_ALLOW_PRIVATE_NETWORK"
	switch value := ms.Env.Getenv(key); value {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	default:
		return false, xmain.UsageErrorf(`invalid environment variable %s. Expected bool. Found "%s".`, key, value)
	}
}

func Run(ctx context.Context, ms *xmain.State) (err error) {
	ctx = log.WithDefault(ctx)
	// Protocol servers own stdin and stdout and parse their own flags.
	if len(ms.Opts.Args) > 0 {
		switch ms.Opts.Args[0] {
		case "mcp":
			return mcpCmd(ctx, ms, ms.Opts.Args[1:])
		case "lsp":
			return lspCmd(ctx, ms, ms.Opts.Args[1:])
		}
	}

	// These should be kept up-to-date with the d2 man page
	watchFlag, err := ms.Opts.Bool("D2_WATCH", "watch", "w", false, "watch for changes to input and live reload. Use $HOST and $PORT to specify the listening address.\n(default localhost:0, which will open on a randomly available local port).")
	if err != nil {
		return err
	}
	hostFlag := ms.Opts.String("HOST", "host", "h", "localhost", "host listening address when used with watch")
	portFlag := ms.Opts.String("PORT", "port", "p", "0", "port listening address when used with watch")
	bundleFlag, err := ms.Opts.Bool("D2_BUNDLE", "bundle", "b", true, "when outputting SVG, bundle all assets and layers into the output file")
	if err != nil {
		return err
	}
	allowPrivateNetworkDefault, err := privateNetworkEnvDefault(ms)
	if err != nil {
		return err
	}
	allowPrivateNetworkFlag, err := ms.Opts.Bool("", "allow-private-network", "", allowPrivateNetworkDefault, "allow remote image assets to access private, loopback, and link-local networks. Only enable this for trusted diagrams. Can also be set with $D2_ALLOW_PRIVATE_NETWORK")
	if err != nil {
		return err
	}
	allowLocalFilesFlag, err := ms.Opts.Bool("D2_ALLOW_LOCAL_FILES", "allow-local-files", "", true, "allow the diagram to read local files: imports, and images bundled into or rendered in the output. Disable it to render untrusted diagrams")
	if err != nil {
		return err
	}
	forceAppendixFlag, err := ms.Opts.Bool("D2_FORCE_APPENDIX", "force-appendix", "", false, "add a numbered appendix for tooltips and links to SVG and video exports and storyboard panels. Ordinary PNG exports include an appendix by default. Also configurable through vars.d2-config.force-appendix.")
	if err != nil {
		return err
	}
	debugFlag, err := ms.Opts.Bool("DEBUG", "debug", "d", false, "print debug logs.")
	if err != nil {
		ms.Log.Warn.Printf("Invalid DEBUG flag value ignored")
		debugFlag = go2.Pointer(false)
	}
	imgCacheFlag, err := ms.Opts.Bool("IMG_CACHE", "img-cache", "", true, "in watch mode, images used in icons are cached for subsequent compilations. This should be disabled if images might change.")
	if err != nil {
		return err
	}
	layoutFlag := ms.Opts.String("D2_LAYOUT", "layout", "l", "dagre", `the layout engine used`)
	themeArg := ms.Opts.String("D2_THEME", "theme", "t", "0", "the diagram theme ID or name (see d2 themes)")
	darkThemeArg := ms.Opts.String("D2_DARK_THEME", "dark-theme", "", "-1", "the theme ID or name to use when the viewer's browser is in dark mode. When left unset -theme is used for both light and dark mode. Be aware that explicit styles set in D2 code will still be applied and this may produce unexpected results. We plan on resolving this by making style maps in D2 light/dark mode specific. See https://github.com/d2lang/d2/issues/831.")
	padFlag, err := ms.Opts.Int64("D2_PAD", "pad", "", d2svg.DEFAULT_PADDING, "pixels padded around the rendered diagram")
	if err != nil {
		return err
	}
	animateIntervalFlag, err := ms.Opts.Int64("D2_ANIMATE_INTERVAL", "animate-interval", "", 0, "minimum milliseconds per board in an animated SVG, GIF, MP4, or WebM. A positive interval plays all exported boards, including linked layers, and packages multiple boards into one SVG. Videos and multiboard GIFs default to 1000ms per board. GIFs allow time for icon departures and derive a single-board loop from the animation. Also configurable through vars.d2-config.animate-interval.")
	if err != nil {
		return err
	}
	animateDurationFlag, err := ms.Opts.Int64("D2_ANIMATE_DURATION", "animate-duration", "", 0, "milliseconds to animate transitions into and out of linked layers and step changes to numeric properties, solid colors, compatible gradients, variables, grid layouts, and supported TALA structural edits (0 disables interpolation). Also configurable through vars.d2-config.animate-duration.")
	if err != nil {
		return err
	}
	animateProgressFlag, err := ms.Opts.Bool("D2_ANIMATE_PROGRESS", "animate-progress", "", false, "show playback progress along the bottom of animated SVG, GIF, MP4, and WebM exports. Also configurable through vars.d2-config.animate-progress.")
	if err != nil {
		return err
	}
	if ms.Env.Getenv("D2_ANIMATE_PROGRESS") == "" {
		ms.Opts.Flags.Lookup("animate-progress").DefValue = "on with a positive animate-interval"
	}
	animateProgressColor := ms.Opts.String("D2_ANIMATE_PROGRESS_COLOR", "animate-progress-color", "", "", "color of the playback progress line (an opaque named color, #RGB, or #RRGGBB). Defaults to the theme's B1 color. Also configurable through vars.d2-config.animate-progress-color.")
	videoDuration := ms.Opts.Flags.Duration("video-duration", 0, "total video playback duration (e.g. 10s or 250ms). By default a video plays the board sequence once. A shorter duration cuts it, and a longer duration repeats it.")
	videoSizeFlag := ms.Opts.Flags.String("video-size", "", "video canvas in pixels, e.g. 1920x1080. Fits and centers the diagram at this resolution.")
	videoQualityFlag := ms.Opts.Flags.String("video-quality", "high", "video encoding quality: low, medium, high, or best.")
	timeoutFlag, err := ms.Opts.Int64("D2_TIMEOUT", "timeout", "", 120, "the maximum number of seconds that D2 runs for before timing out and exiting. Set this option to apply a timeout to video exports.")
	if err != nil {
		return err
	}

	versionFlag, err := ms.Opts.Bool("", "version", "v", false, "get the version")
	if err != nil {
		return err
	}
	sketchFlag, err := ms.Opts.Bool("D2_SKETCH", "sketch", "s", false, "render the diagram to look like it was sketched by hand")
	if err != nil {
		return err
	}
	stdoutFormatFlag := ms.Opts.String("D2_STDOUT_FORMAT", "stdout-format", "", "", "output format when writing to stdout (svg, png, ascii, txt, pdf, pptx, gif, mp4, webm). Usage: d2 input.d2 --stdout-format svg - > output.svg")
	if err != nil {
		return err
	}

	browserFlag := ms.Opts.String("BROWSER", "browser", "", "", "browser executable that watch opens. Setting to 0 opens no browser.")
	centerFlag, err := ms.Opts.Bool("D2_CENTER", "center", "c", false, "center the SVG in the containing viewbox, such as your browser screen")
	if err != nil {
		return err
	}
	scaleFlag, err := ms.Opts.Float64("SCALE", "scale", "", -1, "scale the output. E.g., 0.5 to halve the default size. Default -1 means that SVG's will fit to screen and all others will use their default render size. Setting to 1 turns off SVG fitting to screen. Also configurable through vars.d2-config.scale.")
	if err != nil {
		return err
	}
	targetFlag := ms.Opts.String("", "target", "", "*", "target board to render. Pass an empty string to target root board. If target ends with '*', it will be rendered with all of its scenarios, steps, and layers. Otherwise, only the target board will be rendered. E.g. --target='' to render root board only or --target='layers.x.*' to render layer 'x' with all of its children.")

	fontRegularFlag := ms.Opts.String("D2_FONT_REGULAR", "font-regular", "", "", "path to .ttf file to use for the regular font. If none provided, Source Sans Pro Regular is used.")
	fontItalicFlag := ms.Opts.String("D2_FONT_ITALIC", "font-italic", "", "", "path to .ttf file to use for the italic font. If none provided, Source Sans Pro Regular-Italic is used.")
	fontBoldFlag := ms.Opts.String("D2_FONT_BOLD", "font-bold", "", "", "path to .ttf file to use for the bold font. If none provided, Source Sans Pro Bold is used.")
	fontSemiboldFlag := ms.Opts.String("D2_FONT_SEMIBOLD", "font-semibold", "", "", "path to .ttf file to use for the semibold font. If none provided, Source Sans Pro Semibold is used.")
	fontMonoFlag := ms.Opts.String("D2_FONT_MONO", "font-mono", "", "", "path to .ttf file to use for the monospace font. If none provided, Source Code Pro Regular is used.")
	fontMonoBoldFlag := ms.Opts.String("D2_FONT_MONO_BOLD", "font-mono-bold", "", "", "path to .ttf file to use for the monospace bold font. If none provided, Source Code Pro Bold is used.")
	fontMonoItalicFlag := ms.Opts.String("D2_FONT_MONO_ITALIC", "font-mono-italic", "", "", "path to .ttf file to use for the monospace italic font. If none provided, Source Code Pro Italic is used.")
	fontMonoSemiboldFlag := ms.Opts.String("D2_FONT_MONO_SEMIBOLD", "font-mono-semibold", "", "", "path to .ttf file to use for the monospace semibold font. If none provided, Source Code Pro Semibold is used.")

	checkFlag, err := ms.Opts.Bool("D2_CHECK", "check", "", false, "with fmt, exit with status 1 if an input needs formatting")
	if err != nil {
		return err
	}
	fmtDiffFlag, err := ms.Opts.Bool("", "diff", "", false, "with fmt, print a unified diff instead of writing formatted input")
	if err != nil {
		return err
	}

	noXMLTagFlag, err := ms.Opts.Bool("D2_NO_XML_TAG", "no-xml-tag", "", false, "omit XML tag (<?xml ...?>) from output SVG files. Useful when generating SVGs for direct HTML embedding")
	if err != nil {
		return err
	}

	saltFlag := ms.Opts.String("", "salt", "", "", "Add a salt value to ensure the output uses unique IDs. This is useful when generating multiple identical diagrams to be included in the same HTML doc, so that duplicate IDs do not cause invalid HTML. The salt value is a string that will be appended to IDs in the output.")

	omitVersionFlag, err := ms.Opts.Bool("OMIT_VERSION", "omit-version", "", false, "omit D2 version from generated image")
	if err != nil {
		return err
	}

	asciiModeFlag := ms.Opts.String("D2_ASCII_MODE", "ascii-mode", "", "extended", "ASCII rendering mode for text outputs. Options: 'standard' (basic ASCII chars) or 'extended' (Unicode chars)")
	if err != nil {
		return err
	}

	motionFlag := ms.Opts.Flags.Bool("motion", false, "with lint, check the playback timeline for motion and readability problems")

	populateLayoutOpts(ms)

	err = ms.Opts.Flags.Parse(ms.Opts.Args)
	if !errors.Is(err, pflag.ErrHelp) && err != nil {
		return xmain.UsageErrorf("failed to parse flags: %v", err)
	}

	if errors.Is(err, pflag.ErrHelp) {
		help(ms)
		return nil
	}
	ctx = netpolicy.WithPolicy(ctx, netpolicy.Policy{AllowPrivateNetworks: *allowPrivateNetworkFlag})
	// The CLI is a trusted local application and, by default, preserves its
	// historical ability to read arbitrary host files. --allow-local-files=false
	// denies every local-file access, for untrusted diagrams.
	localFiles := localfile.Unrestricted()
	if !*allowLocalFilesFlag {
		localFiles = localfile.Policy{}
	}
	ctx = withLocalFiles(ctx, localFiles)

	fontFamily, monoFontFamily, err := loadFonts(ms, *fontRegularFlag, *fontItalicFlag, *fontBoldFlag, *fontSemiboldFlag, *fontMonoFlag, *fontMonoBoldFlag, *fontMonoItalicFlag, *fontMonoSemiboldFlag)
	if err != nil {
		return xmain.UsageErrorf("failed to load specified fonts: %v", err)
	}

	args := ms.Opts.Flags.Args()
	review := ""
	if *motionFlag && (len(args) == 0 || args[0] != "lint") {
		return xmain.UsageErrorf("--motion is used with d2 lint")
	}
	if len(args) > 0 {
		switch args[0] {
		case "storyboard", "lint":
			review = args[0]
			args, err = motionReviewArgs(review, args[1:], *motionFlag)
			if err != nil {
				return err
			}
			if *watchFlag || *targetFlag != "*" {
				return xmain.UsageErrorf("d2 %s reviews the complete playback and does not take --watch or --target", review)
			}
			if review == "storyboard" && ms.Opts.Flags.Changed("scale") {
				return xmain.UsageErrorf("storyboard fits each frame to a fixed-size panel; omit --scale")
			}
		case "layout":
			return layoutCmd(ctx, ms)
		case "themes":
			themesCmd(ctx, ms)
			return nil
		case "fmt":
			return fmtCmd(ctx, ms, *checkFlag, *fmtDiffFlag)
		case "play":
			return playCmd(ctx, ms, *themeArg)
		case "validate":
			return validateCmd(ctx, ms)
		case "version":
			if len(ms.Opts.Flags.Args()) > 1 {
				return xmain.UsageErrorf("version subcommand accepts no arguments")
			}
			fmt.Println(version.Version)
			return nil
		}
	}

	if *debugFlag {
		ctx = log.Leveled(ctx, slog.LevelDebug)
		ms.Env.Setenv("DEBUG", "1")
	}
	if *imgCacheFlag {
		ms.Env.Setenv("IMG_CACHE", "1")
	}
	if *browserFlag != "" {
		ms.Env.Setenv("BROWSER", *browserFlag)
	}

	var inputPath string
	var outputPath string

	if len(args) == 0 {
		if versionFlag != nil && *versionFlag {
			fmt.Println(version.Version)
			return nil
		}
		help(ms)
		return nil
	} else if len(args) >= 3 {
		return xmain.UsageErrorf("too many arguments passed")
	}

	inputPath = args[0]
	if len(args) == 2 {
		outputPath = args[1]
	} else {
		if inputPath == "-" {
			outputPath = "-"
		} else {
			outputPath = renameExt(inputPath, ".svg")
		}
	}
	if inputPath != "-" {
		inputPath = ms.AbsPath(inputPath)
		d, err := os.Stat(inputPath)
		if err == nil && d.IsDir() {
			inputPath = filepath.Join(inputPath, "index.d2")
		}
	}
	if filepath.Ext(outputPath) == ".ppt" {
		return xmain.UsageErrorf("D2 does not support ppt exports, did you mean \"pptx\"?")
	}

	// An explicit --stdout-format has historically also selected the format of
	// named files. A shell-wide default only applies when writing to stdout.
	if outputPath != "-" && !ms.Opts.Flags.Changed("stdout-format") {
		stdoutFormatFlag = nil
	}
	outputFormat := PNG
	if review == "" {
		outputFormat, err = getOutputFormat(stdoutFormatFlag, outputPath)
		if err != nil {
			return xmain.UsageErrorf("%v", err)
		}
	} else if ms.Opts.Flags.Changed("stdout-format") && (review != "storyboard" || !strings.EqualFold(*stdoutFormatFlag, "png")) {
		return xmain.UsageErrorf("--stdout-format does not apply to d2 %s", review)
	}
	if err := d2target.ValidateAnimateProgressColor(*animateProgressColor); err != nil {
		return xmain.UsageErrorf("--animate-progress-color: %v", err)
	}
	video := videoOptions{duration: *videoDuration, quality: videoQuality(*videoQualityFlag)}
	for _, name := range []string{"video-duration", "video-size", "video-quality"} {
		if ms.Opts.Flags.Changed(name) && !outputFormat.isVideo() {
			return xmain.UsageErrorf("--%s requires MP4 or WebM output", name)
		}
	}
	if ms.Opts.Flags.Changed("video-duration") && video.duration <= 0 {
		return xmain.UsageErrorf("--video-duration must be positive")
	}
	if ms.Opts.Flags.Changed("video-size") {
		video.size, err = parseVideoSize(*videoSizeFlag)
		if err != nil {
			return xmain.UsageErrorf("--video-size: %v", err)
		}
		if ms.Opts.Flags.Changed("scale") && *scaleFlag > 0 {
			return xmain.UsageErrorf("--scale and --video-size both set the video resolution; choose one")
		}
	}
	if ms.Opts.Flags.Changed("video-quality") {
		if _, ok := video.quality.crf(outputFormat); !ok || video.quality == "" {
			return xmain.UsageErrorf("--video-quality must be low, medium, high, or best")
		}
	}
	if *animateDurationFlag < 0 || *animateDurationFlag > d2target.MaxTweenDurationMS {
		return xmain.UsageErrorf("--animate-duration must be between 0 and %d milliseconds", d2target.MaxTweenDurationMS)
	}
	if review == "" {
		if err := validateAnimateInterval(*animateIntervalFlag, outputFormat); err != nil {
			return err
		}
	}
	if outputPath != "-" {
		outputPath = ms.AbsPath(outputPath)
		if inputPath != "-" && sameInputAndOutput(inputPath, outputPath) {
			return xmain.UsageErrorf("input and output refer to the same file: %s", ms.HumanPath(inputPath))
		}
	}

	themeFlag, err := resolveThemeFlag(ms, "theme", *themeArg)
	if err != nil {
		return err
	}

	// If flag is not explicitly set by user, set to nil.
	// Later, configs from D2 code will only overwrite if they weren't explicitly set by user
	flagSet := make(map[string]struct{})
	ms.Opts.Flags.Visit(func(f *pflag.Flag) {
		flagSet[f.Name] = struct{}{}
	})
	if !ms.Opts.Flags.Changed("animate-interval") && ms.Env.Getenv("D2_ANIMATE_INTERVAL") == "" {
		animateIntervalFlag = nil
	}
	if !ms.Opts.Flags.Changed("animate-duration") && ms.Env.Getenv("D2_ANIMATE_DURATION") == "" {
		animateDurationFlag = nil
	}
	if !ms.Opts.Flags.Changed("animate-progress") && ms.Env.Getenv("D2_ANIMATE_PROGRESS") == "" {
		animateProgressFlag = nil
	}
	if !ms.Opts.Flags.Changed("force-appendix") && ms.Env.Getenv("D2_FORCE_APPENDIX") == "" {
		forceAppendixFlag = nil
	}
	if !ms.Opts.Flags.Changed("scale") && ms.Env.Getenv("SCALE") == "" {
		scaleFlag = nil
	}
	if ms.Env.Getenv("D2_LAYOUT") == "" {
		if _, ok := flagSet["layout"]; !ok {
			layoutFlag = nil
		}
	}
	if ms.Env.Getenv("D2_THEME") == "" {
		if _, ok := flagSet["theme"]; !ok {
			themeFlag = nil
		}
	}
	if ms.Env.Getenv("D2_SKETCH") == "" {
		if _, ok := flagSet["sketch"]; !ok {
			sketchFlag = nil
		}
	}
	if ms.Env.Getenv("D2_PAD") == "" {
		if _, ok := flagSet["pad"]; !ok {
			padFlag = nil
		}
	}
	if ms.Env.Getenv("D2_CENTER") == "" {
		if _, ok := flagSet["center"]; !ok {
			centerFlag = nil
		}
	}

	darkThemeFlag, err := resolveThemeFlag(ms, "dark-theme", *darkThemeArg)
	if err != nil {
		return err
	}

	if !outputFormat.supportsDarkTheme() {
		if darkThemeFlag != nil {
			ms.Log.Warn.Printf("--dark-theme cannot be used while exporting to another format other than .svg")
			darkThemeFlag = nil
		}
	}
	renderOpts := d2svg.RenderOpts{
		AnimateProgress:      animateProgressFlag,
		AnimateProgressColor: *animateProgressColor,
		Pad:                  padFlag,
		Sketch:               sketchFlag,
		Center:               centerFlag,
		ThemeID:              themeFlag,
		DarkThemeID:          darkThemeFlag,
		Scale:                scaleFlag,
		NoXMLTag:             noXMLTagFlag,
		Salt:                 saltFlag,
		OmitVersion:          omitVersionFlag,
	}

	timeout := time.Duration(*timeoutFlag) * time.Second
	if outputFormat.isVideo() {
		if !ms.Opts.Flags.Changed("timeout") && ms.Env.Getenv("D2_TIMEOUT") == "" {
			timeout = 0
		}
	} else if *watchFlag {
		// Other watch formats have historically compiled without a deadline.
		timeout = 0
	}
	if !*watchFlag && timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if review != "" {
		return runMotionReview(ctx, ms, review, inputPath, outputPath, layoutFlag, fontFamily, monoFontFamily, animateIntervalFlag, animateDurationFlag, forceAppendixFlag, renderOpts)
	}

	if *watchFlag {
		if inputPath == "-" {
			return xmain.UsageErrorf("-w[atch] cannot be combined with reading input from stdin")
		}
		if *targetFlag != "*" {
			return xmain.UsageErrorf("-w[atch] cannot be combined with --target")
		}
		w, err := newWatcher(ctx, ms, watcherOpts{
			layout:          layoutFlag,
			renderOpts:      renderOpts,
			animateInterval: animateIntervalFlag,
			animateDuration: animateDurationFlag,
			video:           video,
			compileTimeout:  timeout,
			host:            *hostFlag,
			port:            *portFlag,
			inputPath:       inputPath,
			outputPath:      outputPath,
			bundle:          *bundleFlag,
			forceAppendix:   forceAppendixFlag,
			fontFamily:      fontFamily,
			monoFontFamily:  monoFontFamily,
			outputFormat:    outputFormat,
			asciiMode:       *asciiModeFlag,
		})
		if err != nil {
			return err
		}
		return w.run()
	}

	var boardPath []string
	var noChildren bool
	switch *targetFlag {
	case "*":
	case "":
		noChildren = true
	default:
		target := *targetFlag
		if strings.HasSuffix(target, ".*") {
			target = target[:len(target)-2]
		} else {
			noChildren = true
		}
		key, err := d2parser.ParseKey(target)
		if err != nil {
			return xmain.UsageErrorf("invalid target: %s", *targetFlag)
		}
		boardPath = key.StringIDA()
	}

	_, written, err := compile(ctx, ms, localFilesFromContext(ctx), layoutFlag, &renderOpts, fontFamily, monoFontFamily, animateIntervalFlag, animateDurationFlag, video, inputPath, outputPath, boardPath, noChildren, *bundleFlag, forceAppendixFlag, outputFormat, *asciiModeFlag, nil)
	if err != nil {
		if written {
			return fmt.Errorf("failed to fully compile (partial render written) %s: %w", ms.HumanPath(inputPath), err)
		}
		return fmt.Errorf("failed to compile %s: %w", ms.HumanPath(inputPath), err)
	}
	return nil
}

func sameInputAndOutput(input, output string) bool {
	if input == output {
		return true
	}
	source, err := os.Stat(input)
	if err != nil {
		return false
	}
	destination, err := os.Stat(output)
	return err == nil && os.SameFile(source, destination)
}

func validateAnimateInterval(animateInterval int64, ext exportExtension) error {
	if animateInterval < 0 {
		return xmain.UsageErrorf("--animate-interval must be non-negative")
	}
	if int64(int(animateInterval)) != animateInterval {
		return xmain.UsageErrorf("animate-interval exceeds the supported range on this platform")
	}
	if animateInterval > 0 && !ext.supportsAnimation() {
		return xmain.UsageErrorf("--animate-interval can only be used when exporting to SVG, GIF, MP4, or WebM.\nYou provided: %s", ext)
	}
	return nil
}

func cliLayoutEngine(engine string, ext exportExtension) string {
	// Text output substitutes ELK after the compiler resolves CLI, environment
	// and source configuration, including imported configuration.
	if ext == TXT && strings.EqualFold(engine, "dagre") {
		return "elk"
	}
	return engine
}

func newCLICompileOptions(ctx context.Context, ms *xmain.State, fs fs.FS, inputPath string, layout *string, fontFamily, monoFontFamily *d2fonts.FontFamily, animateDurationFlag *int64, ext exportExtension) (*d2lib.CompileOptions, error) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, err
	}

	layoutResolver := LayoutResolver(ctx, ms)
	routerResolver := RouterResolver(ctx, ms)
	return &d2lib.CompileOptions{
		Ruler:           ruler,
		FontFamily:      fontFamily,
		MonoFontFamily:  monoFontFamily,
		InputPath:       inputPath,
		LayoutResolver:  func(engine string) (d2graph.LayoutGraph, error) { return layoutResolver(cliLayoutEngine(engine, ext)) },
		Layout:          layout,
		RouterResolver:  func(engine string) (d2graph.RouteEdges, error) { return routerResolver(cliLayoutEngine(engine, ext)) },
		FS:              fs,
		LayoutReuse:     true,
		AnimateDuration: animateDurationFlag,

		LayoutContinuations: map[string]d2graph.LayoutContinuation{"tala": d2talalayout.ContinueBoard},
		LayoutTweens:        map[string]d2graph.LayoutTween{"tala": d2talalayout.TweenResize},
	}, nil
}

type videoOptions struct {
	duration time.Duration
	size     videoSize
	quality  videoQuality
}

func compile(ctx context.Context, ms *xmain.State, fs fs.FS, layout *string, renderOptions *d2svg.RenderOpts, fontFamily *d2fonts.FontFamily, monoFontFamily *d2fonts.FontFamily, animateIntervalFlag, animateDurationFlag *int64, video videoOptions, inputPath, outputPath string, boardPath []string, noChildren, bundle bool, forceAppendixFlag *bool, ext exportExtension, asciiMode string, watchURLPath *string) (_ []byte, written bool, _ error) {
	wantPreview := watchURLPath != nil

	start := time.Now()
	input, err := ms.ReadPath(inputPath)
	if err != nil {
		return nil, false, err
	}

	opts, err := newCLICompileOptions(ctx, ms, fs, inputPath, layout, fontFamily, monoFontFamily, animateDurationFlag, ext)
	if err != nil {
		return nil, false, err
	}
	ruler := opts.Ruler

	// With no SVG interval override, compilation reads the interval from d2-config.
	switch {
	case noChildren || !ext.supportsAnimation():
		opts.AnimateTweens = go2.Pointer(false)
	case ext == GIF || ext.isVideo():
		opts.AnimateTweens = go2.Pointer(true)
	case animateIntervalFlag != nil:
		opts.AnimateTweens = go2.Pointer(*animateIntervalFlag > 0)
	}

	if os.Getenv("D2_LSP_MODE") == "1" {
		// only the parse result is needed if running d2 for lsp,
		// if this, "fails", the AST is still valid and can be sent
		// to vscode extension
		ast, err := d2lib.Parse(ctx, string(input), opts)

		type LspOutputData struct {
			Ast *d2ast.Map
			Err error
		}
		jsonOutput, err := json.Marshal(LspOutputData{Ast: ast, Err: err})
		if err != nil {
			return nil, false, err
		}
		fmt.Print(string(jsonOutput))
		os.Exit(42)
		return nil, false, nil
	}

	cancel := background.Repeat(func() {
		ms.Log.Info.Printf("compiling & running layout algorithms...")
	}, time.Second*5)
	defer cancel()

	rootDiagram, g, err := d2lib.Compile(ctx, string(input), opts, renderOptions)
	if err != nil {
		return nil, false, err
	}
	var animateInterval int64
	if interval := rootDiagram.Config.ResolveAnimateInterval(animateIntervalFlag); interval != nil {
		animateInterval = *interval
		if err := validateAnimateInterval(animateInterval, ext); err != nil {
			return nil, false, err
		}
	}
	progress := rootDiagram.Config.ResolveAnimateProgress(renderOptions.AnimateProgress, animateInterval > 0)
	if ext.isVideo() && animateInterval == 0 {
		animateInterval = 1000
		ms.Log.Debug.Printf("%s export: defaulting animate-interval to 1000ms", ext)
	}
	appendixOption := rootDiagram.Config.ResolveForceAppendix(forceAppendixFlag)
	forceAppendix := appendixOption != nil && *appendixOption
	// The CLI's automatic scale sentinel must survive source option resolution.
	// Ordinary exports historically treat nonpositive values and NaN as auto.
	if renderOptions.Scale != nil && !(*renderOptions.Scale > 0) {
		renderOptions.Scale = nil
	}
	if ext.isVideo() && video.size != (videoSize{}) {
		renderOptions.Scale = nil
	}
	renderOpts := *renderOptions
	renderOpts.AnimateProgress = &progress
	cancel()

	engine := cliLayoutEngine(*opts.Layout, ext)
	if (ext == GIF || ext.isVideo()) && strings.EqualFold(engine, "tala") {
		markPinnedGIFBoards(rootDiagram, g)
	}
	diagram, resolvedPath := rootDiagram.ResolveBoard(boardPath)
	if diagram == nil {
		return nil, false, fmt.Errorf(`render target "%s" not found`, strings.Join(boardPath, "."))
	}
	if noChildren {
		diagram.Layers = nil
		diagram.Scenarios = nil
		diagram.Steps = nil
	}

	if animateInterval > 0 || ext == GIF {
		masterID, err := diagram.HashID(renderOpts.Salt)
		if err != nil {
			return nil, false, err
		}
		renderOpts.MasterID = masterID
	}

	if !isBuiltinLayout(engine) {
		return nil, false, layoutNotFound(engine)
	}
	ms.Log.Debug.Printf("using layout engine %s (built-in)", strings.ToLower(engine))
	if err := d2layoutfeatures.Check(engine, g); err != nil {
		return nil, false, err
	}

	switch ext {
	case GIF, MP4, WEBM:
		layerDuration, err := d2animate.LayerDuration(rootDiagram)
		if err != nil {
			return nil, false, err
		}
		cacheImages := ms.Env.Getenv("IMG_CACHE") == "1"
		layers := gifLayerOptions{boardPath: resolvedPath, duration: layerDuration}
		if ext.isVideo() {
			var preview []byte
			written, err := writeVideoWithStatus(ctx, ms, outputPath, ext, video.quality, func(emit func(image.Image, int) error) error {
				if wantPreview {
					emit = withVideoWatchPreview(ctx, renderOpts.Scale, &preview, emit)
				}
				return renderVideoFrames(ctx, inputPath, cacheImages, diagram, renderOpts, int(animateInterval), layers, video.duration, forceAppendix, video.size, emit)
			})
			if err != nil {
				return nil, written, err
			}
			ms.Log.Success.Printf("successfully compiled %s to %s in %s", ms.HumanPath(inputPath), ms.HumanPath(outputPath), time.Since(start))
			return preview, written, nil
		}
		out, previewSVG, err := renderGIF(ctx, inputPath, cacheImages, diagram, renderOpts, int(animateInterval), wantPreview, layers)
		if err != nil {
			return nil, false, err
		}
		if wantPreview {
			previewSVG, err = appendRasterPreview(diagram, renderOpts, ruler, previewSVG)
			if err != nil {
				return nil, false, err
			}
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return nil, false, err
		}
		outputWritten, err := runStatusFinalizer(ctx, func() (bool, error) {
			return writeWithStatus(ms, outputPath, out)
		})
		if err != nil {
			return nil, outputWritten, err
		}
		dur := time.Since(start)
		ms.Log.Success.Printf("successfully compiled %s to %s in %s", ms.HumanPath(inputPath), ms.HumanPath(outputPath), dur)
		return previewSVG, true, nil
	case PDF:
		path := []pdf.BoardTitle{
			{Name: diagram.Root.Label, BoardID: "root"},
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return nil, false, err
		}
		cacheImages := ms.Env.Getenv("IMG_CACHE") == "1"
		var preview []byte
		var outputWritten bool
		if outputPath == "-" {
			var output bytes.Buffer
			preview, err = renderPDFTo(ctx, renderOpts, inputPath, &output, cacheImages, ruler, diagram, path, diagram.Root.Label != "", wantPreview)
			if err == nil {
				outputWritten, err = runStatusFinalizer(ctx, func() (bool, error) {
					return writeStdout(ms.Stdout, output.Bytes())
				})
			}
		} else {
			preview, outputWritten, err = renderPDFWithStatus(ctx, renderOpts, inputPath, outputPath, cacheImages, ruler, diagram, path, diagram.Root.Label != "", wantPreview)
		}
		if err != nil {
			return preview, outputWritten, err
		}
		dur := time.Since(start)
		ms.Log.Success.Printf("successfully compiled %s to %s in %s", ms.HumanPath(inputPath), ms.HumanPath(outputPath), dur)
		return preview, true, nil
	case PPTX:
		var username string
		if user, err := user.Current(); err == nil {
			username = user.Username
		}
		description := "Presentation generated with D2 - https://d2lang.com"
		rootName := getFileName(outputPath)
		if outputPath == "-" {
			rootName = getFileName(inputPath)
			if inputPath == "-" {
				rootName = "stdin"
			}
		}
		// version must be only numbers to avoid issues with PowerPoint
		p := pptx.NewPresentation(rootName, description, rootName, username, version.OnlyNumbers(), diagram.Root.Label != "")

		path := []pptx.BoardTitle{
			{Name: "root", BoardID: "root"},
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return nil, false, err
		}
		cacheImages := ms.Env.Getenv("IMG_CACHE") == "1"
		preview, err := renderPPTX(ctx, p, renderOpts, inputPath, cacheImages, ruler, diagram, path, wantPreview)
		if err != nil {
			return preview, false, err
		}
		if outputPath == "-" {
			var output bytes.Buffer
			if err := runFinalizer(ctx, func() error { return p.ExportTo(&output) }); err != nil {
				return preview, false, err
			}
			outputWritten, err := runStatusFinalizer(ctx, func() (bool, error) {
				return writeStdout(ms.Stdout, output.Bytes())
			})
			if err != nil {
				return preview, outputWritten, err
			}
		} else {
			outputWritten, err := runStatusFinalizer(ctx, func() (bool, error) {
				return p.SaveToWithStatus(outputPath)
			})
			if err != nil {
				return preview, outputWritten, err
			}
		}
		dur := time.Since(start)
		ms.Log.Success.Printf("successfully compiled %s to %s in %s", ms.HumanPath(inputPath), ms.HumanPath(outputPath), dur)
		return preview, true, nil
	default:
		compileDur := time.Since(start)
		if animateInterval > 0 {
			layerDuration, err := d2animate.LayerDuration(rootDiagram)
			if err != nil {
				return nil, false, err
			}
			timeline, err := d2animate.SVGTimeline(diagram, resolvedPath, int(animateInterval), layerDuration)
			if err != nil {
				return nil, false, err
			}
			if len(timeline) == 0 {
				if !noChildren {
					return nil, true, nil
				}
				// An explicitly selected, empty board is still a single image.
				timeline = append(timeline, d2animate.Frame{Diagram: diagram, DurationMS: int(animateInterval)})
			}
			svgs := make([][]byte, 0, len(timeline))
			for _, frame := range timeline {
				if err := ctx.Err(); err != nil {
					return nil, false, err
				}
				svg, _, err := _renderWithPNGEncoder(ctx, ms, renderOpts, inputPath, outputPath, bundle, forceAppendix, ruler, frame.Picture(), ext, asciiMode, wantPreview, nil)
				if err != nil {
					return nil, false, err
				}
				svgs = append(svgs, svg)
			}
			out, err := d2animate.WrapTimeline(diagram, resolvedPath, timeline, svgs, renderOpts, layerDuration)
			if err != nil {
				return nil, false, err
			}
			if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
				return nil, false, err
			}
			written, err := runStatusFinalizer(ctx, func() (bool, error) {
				return writeWithStatus(ms, outputPath, out)
			})
			if err != nil {
				return nil, written, err
			}
			ms.Log.Success.Printf("successfully compiled %s to %s in %s", ms.HumanPath(inputPath), ms.HumanPath(outputPath), time.Since(start))
			return out, written, nil
		}
		// Rename all the "root.layers.x" links to their exported paths.
		linkToOutput, err := resolveLinks("root", outputPath, rootDiagram)
		if err != nil {
			return nil, false, err
		}
		if err := relink("root", rootDiagram, linkToOutput); err != nil {
			return nil, false, err
		}

		var boards [][]byte
		var selection *boardOutputPreview
		if watchURLPath != nil && *watchURLPath != "" {
			selection = &boardOutputPreview{path: watchBoardOutput(*watchURLPath, outputPath, linkToOutput)}
		}
		var outputWritten bool
		if !noChildren {
			if err := validateBoardOutputPaths(outputPath, diagram); err != nil {
				return nil, false, err
			}
		}
		outputPaths := newBoardOutputPaths(outputPath)
		var outputWorkspace *boardOutputWorkspace
		if outputPath != "-" && (len(diagram.Layers) > 0 || len(diagram.Scenarios) > 0 || len(diagram.Steps) > 0) {
			outputWorkspace, err = newBoardOutputWorkspace(outputPath)
			if err != nil {
				return nil, false, err
			}
			outputPaths = outputWorkspace.outputPaths(outputPath)
		}
		if noChildren {
			boards, outputWritten, err = renderSingle(ctx, ms, compileDur, renderOpts, inputPath, outputPath, bundle, forceAppendix, ruler, diagram, ext, asciiMode, wantPreview)
		} else {
			boards, outputWritten, err = renderToPaths(ctx, ms, compileDur, renderOpts, inputPath, outputPaths, bundle, forceAppendix, ruler, diagram, ext, asciiMode, wantPreview && selection == nil, selection)
		}
		if err != nil {
			if outputWorkspace != nil {
				err = errors.Join(err, outputWorkspace.discard())
				outputWritten = false
			}
			return nil, outputWritten, err
		}
		if outputWorkspace != nil {
			outputWritten, err = outputWorkspace.publish()
			if err != nil {
				return nil, outputWritten, err
			}
		}
		if selection != nil {
			if !selection.found {
				return nil, true, fmt.Errorf("preview board %q not found", *watchURLPath)
			}
			return selection.data, true, nil
		}
		var out []byte
		if len(boards) > 0 {
			out = boards[0]
		}
		return out, true, nil
	}
}

func boardLinkID(parent, kind, name string) string {
	return parent + "." + kind + "." + d2format.FormatKeyPath([]d2ast.String{d2ast.RawString(name, true)})
}

func resolveLinks(currDiagramPath, outputPath string, diagram *d2target.Diagram) (linkToOutput map[string]string, err error) {
	plan, err := planBoardOutput(newBoardOutputPaths(outputPath), diagram)
	if err != nil {
		return nil, err
	}

	linkToOutput = map[string]string{currDiagramPath: plan.board.displayPath}

	for _, dl := range diagram.Layers {
		m, err := resolveLinks(boardLinkID(currDiagramPath, "layers", dl.Name), plan.layers.displayPath, dl)
		if err != nil {
			return nil, err
		}
		for k, v := range m {
			linkToOutput[k] = v
		}
	}
	for _, dl := range diagram.Scenarios {
		m, err := resolveLinks(boardLinkID(currDiagramPath, "scenarios", dl.Name), plan.scenarios.displayPath, dl)
		if err != nil {
			return nil, err
		}
		for k, v := range m {
			linkToOutput[k] = v
		}
	}
	for _, dl := range diagram.Steps {
		m, err := resolveLinks(boardLinkID(currDiagramPath, "steps", dl.Name), plan.steps.displayPath, dl)
		if err != nil {
			return nil, err
		}
		for k, v := range m {
			linkToOutput[k] = v
		}
	}

	return linkToOutput, nil
}

func relink(currDiagramPath string, d *d2target.Diagram, linkToOutput map[string]string) error {
	rewrite := func(link *string) error {
		target, ok := linkToOutput[*link]
		if !ok && strings.HasPrefix(*link, "root.") {
			// References may quote ordinary names or a literal dot in a board name.
			if key, err := d2parser.ParseKey(*link); err == nil {
				target, ok = linkToOutput[d2format.FormatKeyPath(key.IDA())]
			}
		}
		if !ok {
			return nil
		}
		rel, err := filepath.Rel(filepath.Dir(linkToOutput[currDiagramPath]), target)
		if err == nil {
			*link = boardOutputLink(rel)
		}
		return err
	}
	for i := range d.Shapes {
		if err := rewrite(&d.Shapes[i].Link); err != nil {
			return err
		}
	}
	for i := range d.Connections {
		if err := rewrite(&d.Connections[i].Link); err != nil {
			return err
		}
	}
	for _, board := range d.Layers {
		err := relink(boardLinkID(currDiagramPath, "layers", board.Name), board, linkToOutput)
		if err != nil {
			return err
		}
	}
	for _, board := range d.Scenarios {
		err := relink(boardLinkID(currDiagramPath, "scenarios", board.Name), board, linkToOutput)
		if err != nil {
			return err
		}
	}
	for _, board := range d.Steps {
		err := relink(boardLinkID(currDiagramPath, "steps", board.Name), board, linkToOutput)
		if err != nil {
			return err
		}
	}
	return nil
}

func boardOutputLink(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, part := range parts {
		if part != "." && part != ".." {
			parts[i] = url.PathEscape(part)
		}
	}
	return strings.Join(parts, "/")
}

// A browser URL selects a preview from the full export. It never supplies a write path.
type boardOutputPreview struct {
	path  string
	data  []byte
	found bool
}

func render(ctx context.Context, ms *xmain.State, compileDur time.Duration, opts d2svg.RenderOpts, inputPath, outputPath string, bundle, forceAppendix bool, ruler *textmeasure.Ruler, diagram *d2target.Diagram, ext exportExtension, asciiMode string, wantPreview bool) (_ [][]byte, written bool, _ error) {
	return renderToPaths(ctx, ms, compileDur, opts, inputPath, newBoardOutputPaths(outputPath), bundle, forceAppendix, ruler, diagram, ext, asciiMode, wantPreview, nil)
}

func renderToPaths(ctx context.Context, ms *xmain.State, compileDur time.Duration, opts d2svg.RenderOpts, inputPath string, outputPaths boardOutputPaths, bundle, forceAppendix bool, ruler *textmeasure.Ruler, diagram *d2target.Diagram, ext exportExtension, asciiMode string, wantPreview bool, selection *boardOutputPreview) (_ [][]byte, written bool, _ error) {
	if ext == PNG {
		var encoder rasterPNGEncoder
		defer encoder.close()
		return renderWithPNGEncoderToPaths(ctx, ms, compileDur, opts, inputPath, outputPaths, bundle, forceAppendix, ruler, diagram, ext, asciiMode, wantPreview, &encoder, selection)
	}
	return renderWithPNGEncoderToPaths(ctx, ms, compileDur, opts, inputPath, outputPaths, bundle, forceAppendix, ruler, diagram, ext, asciiMode, wantPreview, nil, selection)
}

func renderWithPNGEncoder(ctx context.Context, ms *xmain.State, compileDur time.Duration, opts d2svg.RenderOpts, inputPath, outputPath string, bundle, forceAppendix bool, ruler *textmeasure.Ruler, diagram *d2target.Diagram, ext exportExtension, asciiMode string, wantPreview bool, pngEncoder *rasterPNGEncoder) (_ [][]byte, written bool, _ error) {
	return renderWithPNGEncoderToPaths(ctx, ms, compileDur, opts, inputPath, newBoardOutputPaths(outputPath), bundle, forceAppendix, ruler, diagram, ext, asciiMode, wantPreview, pngEncoder, nil)
}

func renderWithPNGEncoderToPaths(ctx context.Context, ms *xmain.State, compileDur time.Duration, opts d2svg.RenderOpts, inputPath string, outputPaths boardOutputPaths, bundle, forceAppendix bool, ruler *textmeasure.Ruler, diagram *d2target.Diagram, ext exportExtension, asciiMode string, wantPreview bool, pngEncoder *rasterPNGEncoder, selection *boardOutputPreview) (_ [][]byte, written bool, _ error) {
	plan, err := planBoardOutput(outputPaths, diagram)
	if err != nil {
		return nil, false, err
	}
	if len(diagram.Layers) > 0 || len(diagram.Scenarios) > 0 || len(diagram.Steps) > 0 {
		if plan.base.writePath == "-" {
			// TODO it can if composed into one
			return nil, false, fmt.Errorf("multiboard output cannot be written to stdout")
		}
	}

	var boards [][]byte
	for _, dl := range diagram.Layers {
		childPreview := wantPreview && diagram.IsFolderOnly && len(boards) == 0
		childrenBoards, childWritten, err := renderWithPNGEncoderToPaths(ctx, ms, compileDur, opts, inputPath, plan.layers, bundle, forceAppendix, ruler, dl, ext, asciiMode, childPreview, pngEncoder, selection)
		written = written || childWritten
		if err != nil {
			return boards, written, err
		}
		boards = append(boards, childrenBoards...)
	}
	for _, dl := range diagram.Scenarios {
		childPreview := wantPreview && diagram.IsFolderOnly && len(boards) == 0
		childrenBoards, childWritten, err := renderWithPNGEncoderToPaths(ctx, ms, compileDur, opts, inputPath, plan.scenarios, bundle, forceAppendix, ruler, dl, ext, asciiMode, childPreview, pngEncoder, selection)
		written = written || childWritten
		if err != nil {
			return boards, written, err
		}
		boards = append(boards, childrenBoards...)
	}
	for _, dl := range diagram.Steps {
		childPreview := wantPreview && diagram.IsFolderOnly && len(boards) == 0
		childrenBoards, childWritten, err := renderWithPNGEncoderToPaths(ctx, ms, compileDur, opts, inputPath, plan.steps, bundle, forceAppendix, ruler, dl, ext, asciiMode, childPreview, pngEncoder, selection)
		written = written || childWritten
		if err != nil {
			return boards, written, err
		}
		boards = append(boards, childrenBoards...)
	}

	if !diagram.IsFolderOnly {
		selected := selection != nil && selection.path == plan.board.displayPath
		start := time.Now()
		out, boardWritten, err := _renderWithPNGEncoder(ctx, ms, opts, inputPath, plan.board.writePath, bundle, forceAppendix, ruler, diagram, ext, asciiMode, wantPreview || selected, pngEncoder)
		if selected {
			selection.data, selection.found = out, true
		}
		written = written || boardWritten
		if err != nil {
			return boards, written, err
		}
		dur := compileDur + time.Since(start)
		if opts.MasterID == "" {
			ms.Log.Success.Printf("successfully compiled %s to %s in %s", ms.HumanPath(inputPath), ms.HumanPath(plan.board.displayPath), dur)
		}
		boards = append([][]byte{out}, boards...)
	}

	return boards, written, nil
}

func renderSingle(ctx context.Context, ms *xmain.State, compileDur time.Duration, opts d2svg.RenderOpts, inputPath, outputPath string, bundle, forceAppendix bool, ruler *textmeasure.Ruler, diagram *d2target.Diagram, outputFormat exportExtension, asciiMode string, wantPreview bool) ([][]byte, bool, error) {
	start := time.Now()
	out, written, err := _renderWithPNGEncoder(ctx, ms, opts, inputPath, outputPath, bundle, forceAppendix, ruler, diagram, outputFormat, asciiMode, wantPreview, nil)
	if err != nil {
		return [][]byte{}, written, err
	}
	dur := compileDur + time.Since(start)
	if opts.MasterID == "" {
		ms.Log.Success.Printf("successfully compiled %s to %s in %s", ms.HumanPath(inputPath), ms.HumanPath(outputPath), dur)
	}
	return [][]byte{out}, written, nil
}

func _renderWithPNGEncoder(ctx context.Context, ms *xmain.State, opts d2svg.RenderOpts, inputPath, outputPath string, bundle, forceAppendix bool, ruler *textmeasure.Ruler, diagram *d2target.Diagram, outputFormat exportExtension, asciiMode string, wantPreview bool, pngEncoder *rasterPNGEncoder) ([]byte, bool, error) {
	if outputFormat == TXT {
		var charsetType charset.Type
		switch asciiMode {
		case "standard":
			charsetType = charset.ASCII
		default: // "extended" or any other value defaults to Unicode
			charsetType = charset.Unicode
		}

		renderOpts := &d2ascii.RenderOpts{
			Scale:   opts.Scale,
			Charset: charsetType,
		}
		asciiArtist := d2ascii.NewASCIIartist()
		ascii, err := asciiArtist.Render(ctx, diagram, renderOpts)
		if err != nil {
			return ascii, false, err
		}
		written, err := writeWithStatus(ms, outputPath, ascii)
		if err != nil {
			return ascii, written, err
		}
		return ascii, written, nil
	}
	toPNG := outputFormat == PNG

	var scale *float64
	if opts.Scale != nil {
		scale = opts.Scale
	} else if toPNG {
		scale = go2.Pointer(1.)
	}
	renderOpts := &d2svg.RenderOpts{
		Pad:                opts.Pad,
		Sketch:             opts.Sketch,
		Center:             opts.Center,
		MasterID:           opts.MasterID,
		ThemeID:            opts.ThemeID,
		DarkThemeID:        opts.DarkThemeID,
		ThemeOverrides:     opts.ThemeOverrides,
		DarkThemeOverrides: opts.DarkThemeOverrides,
		NoXMLTag:           opts.NoXMLTag,
		Salt:               opts.Salt,
		Scale:              scale,
		OmitVersion:        opts.OmitVersion,
	}
	if toPNG {
		returnSVG := wantPreview || opts.MasterID != ""
		svg, err := renderRasterSVG(diagram, *renderOpts, returnSVG)
		if err != nil {
			return svg, false, err
		}
		cacheImages := ms.Env.Getenv("IMG_CACHE") == "1"
		if opts.MasterID == "" {
			written, err := writePNGWithStatus(ctx, ms, outputPath, func(output io.Writer) error {
				svg, err = renderPNGToWriter(ctx, inputPath, cacheImages, diagram, *renderOpts, pngEncoder, output, svg)
				return err
			})
			if err != nil {
				return svg, written, err
			}
			return svg, written, nil
		}
		// Animated SVG composition only needs validation, not retained PNG bytes.
		svg, err = renderPNGToWriter(ctx, inputPath, cacheImages, diagram, *renderOpts, pngEncoder, io.Discard, svg)
		return svg, false, err
	}

	svg, err := d2svg.Render(diagram, renderOpts)
	if err != nil {
		return nil, false, err
	}

	cacheImages := ms.Env.Getenv("IMG_CACHE") == "1"
	l := simplelog.FromCmdLog(ms.Log)
	assetResolver, bundleErr := svgBundleResolver(ctx, inputPath, cacheImages)
	if bundleErr == nil {
		svg, bundleErr = imgbundler.BundleWithResolver(ctx, l, svg, imgbundler.BundleOptions{
			Resolver: assetResolver,
			Local:    true,
			Remote:   bundle,
		})
		assetResolver.CloseIdleConnections()
	}
	if forceAppendix {
		svg, err = appendix.AppendRendered(diagram, renderOpts, ruler, svg)
		if err != nil {
			return nil, false, err
		}
	}

	out := svg
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}

	var written bool
	if opts.MasterID == "" {
		err = os.MkdirAll(filepath.Dir(outputPath), 0755)
		if err != nil {
			return svg, false, err
		}
		written, err = writeWithStatus(ms, outputPath, out)
		if err != nil {
			return svg, written, err
		}
	}
	if bundleErr != nil {
		return svg, written, bundleErr
	}
	return svg, written, nil
}

// newExt must include leading .
func renameExt(fp string, newExt string) string {
	ext := filepath.Ext(fp)
	if ext == "" {
		return fp + newExt
	} else {
		return strings.TrimSuffix(fp, ext) + newExt
	}
}

func getFileName(path string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(filepath.Base(path), ext)
}

func loadFont(ms *xmain.State, path string) ([]byte, error) {
	if filepath.Ext(path) != ".ttf" {
		return nil, fmt.Errorf("expected .ttf file but %s has extension %s", path, filepath.Ext(path))
	}
	ttf, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read font at %s: %v", path, err)
	}
	ms.Log.Info.Printf("font %s loaded", filepath.Base(path))
	return ttf, nil
}

func loadFonts(ms *xmain.State, pathToRegular, pathToItalic, pathToBold, pathToSemibold, pathToMono, pathToMonoBold, pathToMonoItalic, pathToMonoSemibold string) (*d2fonts.FontFamily, *d2fonts.FontFamily, error) {
	if pathToRegular == "" && pathToItalic == "" && pathToBold == "" && pathToSemibold == "" &&
		pathToMono == "" && pathToMonoBold == "" && pathToMonoItalic == "" && pathToMonoSemibold == "" {
		return nil, nil, nil
	}

	var regularTTF []byte
	var italicTTF []byte
	var boldTTF []byte
	var semiboldTTF []byte
	var monoTTF []byte
	var monoBoldTTF []byte
	var monoItalicTTF []byte
	var monoSemiboldTTF []byte

	var err error
	if pathToRegular != "" {
		regularTTF, err = loadFont(ms, pathToRegular)
		if err != nil {
			return nil, nil, err
		}
	}
	if pathToItalic != "" {
		italicTTF, err = loadFont(ms, pathToItalic)
		if err != nil {
			return nil, nil, err
		}
	}
	if pathToBold != "" {
		boldTTF, err = loadFont(ms, pathToBold)
		if err != nil {
			return nil, nil, err
		}
	}
	if pathToSemibold != "" {
		semiboldTTF, err = loadFont(ms, pathToSemibold)
		if err != nil {
			return nil, nil, err
		}
	}

	if pathToMono != "" {
		monoTTF, err = loadFont(ms, pathToMono)
		if err != nil {
			return nil, nil, err
		}
	}
	if pathToMonoBold != "" {
		monoBoldTTF, err = loadFont(ms, pathToMonoBold)
		if err != nil {
			return nil, nil, err
		}
	}
	if pathToMonoItalic != "" {
		monoItalicTTF, err = loadFont(ms, pathToMonoItalic)
		if err != nil {
			return nil, nil, err
		}
	}
	if pathToMonoSemibold != "" {
		monoSemiboldTTF, err = loadFont(ms, pathToMonoSemibold)
		if err != nil {
			return nil, nil, err
		}
	}

	var fontFamily *d2fonts.FontFamily
	var monoFontFamily *d2fonts.FontFamily

	if pathToRegular != "" || pathToItalic != "" || pathToBold != "" || pathToSemibold != "" {
		fontFamily, err = d2fonts.AddFontFamily("custom", regularTTF, italicTTF, boldTTF, semiboldTTF)
		if err != nil {
			return nil, nil, err
		}
	}

	if pathToMono != "" || pathToMonoBold != "" || pathToMonoItalic != "" || pathToMonoSemibold != "" {
		monoFontFamily, err = d2fonts.AddFontFamily("customMono", monoTTF, monoItalicTTF, monoBoldTTF, monoSemiboldTTF)
		if err != nil {
			return nil, nil, err
		}
	}

	return fontFamily, monoFontFamily, nil
}

const LAYERS = "layers"
const STEPS = "steps"
const SCENARIOS = "scenarios"

func appendRasterPreview(diagram *d2target.Diagram, opts d2svg.RenderOpts, ruler *textmeasure.Ruler, sourceSVG []byte) ([]byte, error) {
	if len(sourceSVG) == 0 {
		return nil, nil
	}
	renderOpts := rasterRenderOptions(opts)
	preview, err := appendix.AppendRendered(diagram, &renderOpts, ruler, sourceSVG)
	if err != nil {
		return nil, err
	}
	if int64(len(preview)) > rasterPreviewMaxOutputBytes {
		return nil, fmt.Errorf("raster watch preview output bytes %d exceed limit %d", len(preview), rasterPreviewMaxOutputBytes)
	}
	return preview, nil
}

func Write(ms *xmain.State, path string, out []byte) error {
	_, err := writeWithStatus(ms, path, out)
	return err
}

func writeWithStatus(ms *xmain.State, path string, out []byte) (touched bool, err error) {
	if path == "-" {
		return writeStdout(ms.Stdout, out)
	}
	err = ms.AtomicWritePath(path, out)
	if err == nil {
		return true, nil
	}
	ms.Log.Debug.Printf("atomic write failed: %s, trying non-atomic write", err.Error())
	return writeFileWithStatus(path, out)
}

func writeFileWithStatus(path string, out []byte) (touched bool, err error) {
	output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return false, err
	}
	n, writeErr := output.Write(out)
	if writeErr == nil && n != len(out) {
		writeErr = io.ErrShortWrite
	}
	return true, errors.Join(writeErr, output.Close())
}

// runFinalizer makes context-unaware encoding and output operations honor the
// command deadline before they start and before they report success.
func runFinalizer(ctx context.Context, finalize func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := finalize(); err != nil {
		return err
	}
	return ctx.Err()
}

func runStatusFinalizer(ctx context.Context, finalize func() (bool, error)) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	touched, err := finalize()
	if err != nil {
		return touched, err
	}
	return touched, ctx.Err()
}

func writeStdout(output io.WriteCloser, out []byte) (written bool, err error) {
	n, err := output.Write(out)
	written = n > 0
	if err != nil {
		return written, err
	}
	if n != len(out) {
		return written, io.ErrShortWrite
	}
	return written, output.Close()
}

func init() {
	log.Init()
}
