package d2cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/d2lang/util-go/cmdlog"
	"github.com/d2lang/util-go/go2"
	"github.com/d2lang/util-go/xhttp"
	"github.com/d2lang/util-go/xmain"
	"github.com/d2lang/util-go/xos"

	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/lib/localfile"
	timelib "github.com/d2lang/d2/lib/time"
	"github.com/d2lang/d2/lib/urlenc"
)

const maxServeScriptBytes int64 = 16 << 20

type serveOpts struct {
	host            string
	port            string
	layout          *string
	renderOpts      d2svg.RenderOpts
	animateInterval *int64
	animateDuration *int64
	bundle          bool
	forceAppendix   *bool
	fontFamily      *d2fonts.FontFamily
	monoFontFamily  *d2fonts.FontFamily
	asciiMode       string
	target          string
	requestTimeout  time.Duration
	allowLocalFiles bool
}

type diagramServer struct {
	ctx             context.Context
	ms              *xmain.State
	localFiles      localfile.Policy
	layout          *string
	renderOpts      d2svg.RenderOpts
	animateInterval *int64
	animateDuration *int64
	bundle          bool
	forceAppendix   *bool
	fontFamily      *d2fonts.FontFamily
	monoFontFamily  *d2fonts.FontFamily
	asciiMode       string
	target          string
	requestTimeout  time.Duration
	allowLocalFiles bool
}

type serveRequest struct {
	script          string
	ext             exportExtension
	layout          *string
	renderOpts      d2svg.RenderOpts
	animateInterval *int64
	animateDuration *int64
	bundle          bool
	forceAppendix   *bool
	asciiMode       string
	boardPath       []string
	noChildren      bool
}

type memWriteCloser struct {
	buf *bytes.Buffer
}

func (m *memWriteCloser) Write(p []byte) (int, error) {
	return m.buf.Write(p)
}

func (m *memWriteCloser) Close() error {
	return nil
}

type httpStatusError struct {
	status int
	err    error
}

func (e *httpStatusError) Error() string {
	return e.err.Error()
}

func (e *httpStatusError) Unwrap() error {
	return e.err
}

func badRequestError(msg string, args ...any) error {
	return &httpStatusError{
		status: http.StatusBadRequest,
		err:    fmt.Errorf(msg, args...),
	}
}

func serveCmd(ctx context.Context, ms *xmain.State, opts serveOpts) error {
	if len(ms.Opts.Flags.Args()) > 1 {
		return xmain.UsageErrorf("serve subcommand accepts no arguments")
	}

	localFiles := localfile.Policy{}
	if opts.allowLocalFiles {
		localFiles = localFilesFromContext(ctx)
	}

	l, err := net.Listen("tcp", net.JoinHostPort(opts.host, opts.port))
	if err != nil {
		return err
	}
	ms.Log.Success.Printf("listening on http://%s", l.Addr())

	srv := &diagramServer{
		ctx:             ctx,
		ms:              ms,
		localFiles:      localFiles,
		layout:          opts.layout,
		renderOpts:      opts.renderOpts,
		animateInterval: opts.animateInterval,
		animateDuration: opts.animateDuration,
		bundle:          opts.bundle,
		forceAppendix:   opts.forceAppendix,
		fontFamily:      opts.fontFamily,
		monoFontFamily:  opts.monoFontFamily,
		asciiMode:       opts.asciiMode,
		target:          opts.target,
		requestTimeout:  opts.requestTimeout,
		allowLocalFiles: opts.allowLocalFiles,
	}

	logged := xhttp.Log(ms.Log, srv.handler())
	s := xhttp.NewServer(ms.Log.Warn, logged)
	return xhttp.Serve(ctx, 30*time.Second, s, l)
}

func (s *diagramServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/render/", s.handleRender)
	return mux
}

func (s *diagramServer) handleRender(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	formatName := strings.TrimPrefix(r.URL.Path, "/render/")
	if formatName == "" || strings.Contains(formatName, "/") {
		http.NotFound(w, r)
		return
	}
	ext, ok := parseServeFormat(formatName)
	if !ok {
		http.NotFound(w, r)
		return
	}

	req, err := s.parseRenderRequest(w, r, ext)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		var statusErr *httpStatusError
		if errors.As(err, &statusErr) {
			http.Error(w, statusErr.Error(), statusErr.status)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	out, err := s.render(r.Context(), req)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status := classifyServeRenderError(err)
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", contentTypeForExtension(req.ext))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func parseServeFormat(name string) (exportExtension, bool) {
	switch strings.ToLower(name) {
	case "svg":
		return SVG, true
	case "png":
		return PNG, true
	case "txt", "ascii":
		return TXT, true
	case "gif":
		return GIF, true
	case "pdf":
		return PDF, true
	case "pptx":
		return PPTX, true
	default:
		return "", false
	}
}

func contentTypeForExtension(ext exportExtension) string {
	switch ext {
	case SVG:
		return "image/svg+xml; charset=utf-8"
	case PNG:
		return "image/png"
	case TXT:
		return "text/plain; charset=utf-8"
	case GIF:
		return "image/gif"
	case PDF:
		return "application/pdf"
	case PPTX:
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	default:
		return "application/octet-stream"
	}
}

func (s *diagramServer) parseRenderRequest(w http.ResponseWriter, r *http.Request, baseExt exportExtension) (serveRequest, error) {
	query := r.URL.Query()
	var script string
	if query.Has("script") {
		decoded, err := urlenc.Decode(query.Get("script"))
		if err != nil {
			return serveRequest{}, badRequestError("invalid script parameter: %v", err)
		}
		script = decoded
	} else if r.Method == http.MethodPost {
		limited := http.MaxBytesReader(w, r.Body, maxServeScriptBytes)
		body, err := io.ReadAll(limited)
		if err != nil {
			return serveRequest{}, err
		}
		script = string(body)
	} else {
		return serveRequest{}, badRequestError("missing script query parameter")
	}

	ext := baseExt
	if query.Has("ascii") {
		ascii, err := parseBoolQuery(query, "ascii")
		if err != nil {
			return serveRequest{}, err
		}
		if ascii && ext == SVG {
			ext = TXT
		}
	}

	renderOpts := s.renderOpts

	layout := s.layout
	if query.Has("layout") {
		v := strings.TrimSpace(query.Get("layout"))
		if v == "" {
			layout = nil
		} else {
			if !isBuiltinLayout(v) {
				return serveRequest{}, badRequestError("unsupported layout engine %q", v)
			}
			layout = go2.Pointer(strings.ToLower(v))
		}
	}

	if query.Has("theme") {
		theme, err := d2themescatalog.Resolve(query.Get("theme"))
		if err != nil {
			return serveRequest{}, badRequestError("invalid theme parameter: %v", err)
		}
		renderOpts.ThemeID = go2.Pointer(theme.ID)
	}

	if query.Has("dark-theme") {
		raw := query.Get("dark-theme")
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id == -1 {
			renderOpts.DarkThemeID = nil
		} else {
			theme, err := d2themescatalog.Resolve(raw)
			if err != nil {
				return serveRequest{}, badRequestError("invalid dark-theme parameter: %v", err)
			}
			renderOpts.DarkThemeID = go2.Pointer(theme.ID)
		}
	}
	if !ext.supportsDarkTheme() {
		renderOpts.DarkThemeID = nil
	}

	if query.Has("sketch") {
		v, err := parseBoolQuery(query, "sketch")
		if err != nil {
			return serveRequest{}, err
		}
		renderOpts.Sketch = &v
	}
	if query.Has("pad") {
		v, err := parseInt64Query(query, "pad")
		if err != nil {
			return serveRequest{}, err
		}
		renderOpts.Pad = &v
	}
	if query.Has("center") {
		v, err := parseBoolQuery(query, "center")
		if err != nil {
			return serveRequest{}, err
		}
		renderOpts.Center = &v
	}
	if query.Has("scale") {
		v, err := parseFloat64Query(query, "scale")
		if err != nil {
			return serveRequest{}, err
		}
		renderOpts.Scale = &v
	}
	if query.Has("no-xml-tag") {
		v, err := parseBoolQuery(query, "no-xml-tag")
		if err != nil {
			return serveRequest{}, err
		}
		renderOpts.NoXMLTag = &v
	}
	if query.Has("salt") {
		v := query.Get("salt")
		renderOpts.Salt = &v
	}
	if query.Has("omit-version") {
		v, err := parseBoolQuery(query, "omit-version")
		if err != nil {
			return serveRequest{}, err
		}
		renderOpts.OmitVersion = &v
	}

	bundle := s.bundle
	if query.Has("bundle") {
		v, err := parseBoolQuery(query, "bundle")
		if err != nil {
			return serveRequest{}, err
		}
		bundle = v
	}

	forceAppendix := s.forceAppendix
	if query.Has("force-appendix") {
		v, err := parseBoolQuery(query, "force-appendix")
		if err != nil {
			return serveRequest{}, err
		}
		forceAppendix = &v
	}

	animateInterval := s.animateInterval
	if query.Has("animate-interval") {
		v, err := parseInt64Query(query, "animate-interval")
		if err != nil {
			return serveRequest{}, err
		}
		animateInterval = &v
	}
	if animateInterval != nil {
		if err := validateAnimateInterval(*animateInterval, ext); err != nil {
			return serveRequest{}, badRequestError("%v", err)
		}
	}

	animateDuration := s.animateDuration
	if query.Has("animate-duration") {
		v, err := parseInt64Query(query, "animate-duration")
		if err != nil {
			return serveRequest{}, err
		}
		if v < 0 || v > d2target.MaxTweenDurationMS {
			return serveRequest{}, badRequestError("animate-duration must be between 0 and %d milliseconds", d2target.MaxTweenDurationMS)
		}
		animateDuration = &v
	}

	asciiMode := s.asciiMode
	if query.Has("ascii-mode") {
		v := strings.ToLower(query.Get("ascii-mode"))
		if v != "standard" && v != "extended" {
			return serveRequest{}, badRequestError("invalid ascii-mode %q: expected \"standard\" or \"extended\"", v)
		}
		asciiMode = v
	}

	target := s.target
	if query.Has("target") {
		target = query.Get("target")
	}
	boardPath, noChildren, err := parseTarget(target)
	if err != nil {
		return serveRequest{}, badRequestError("%v", err)
	}

	return serveRequest{
		script:          script,
		ext:             ext,
		layout:          layout,
		renderOpts:      renderOpts,
		animateInterval: animateInterval,
		animateDuration: animateDuration,
		bundle:          bundle,
		forceAppendix:   forceAppendix,
		asciiMode:       asciiMode,
		boardPath:       boardPath,
		noChildren:      noChildren,
	}, nil
}

func (s *diagramServer) render(reqCtx context.Context, req serveRequest) ([]byte, error) {
	ctx := reqCtx
	if ctx == nil {
		ctx = s.ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = withNoLSP(withLocalFiles(ctx, s.localFiles))

	timeout := s.requestTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := timelib.WithTimeout(ctx, timeout)
	defer cancel()

	var out bytes.Buffer
	baseMS := s.ms
	if baseMS == nil {
		baseMS = &xmain.State{}
	}
	env := baseMS.Env
	if env == nil {
		env = xos.NewEnv(nil)
	}
	logger := baseMS.Log
	if logger == nil {
		logger = cmdlog.New(env, io.Discard)
	}
	opts := baseMS.Opts
	if opts == nil {
		opts = xmain.NewOpts(env, nil)
		populateLayoutOpts(&xmain.State{Opts: opts})
	}
	reqMS := &xmain.State{
		Name:   baseMS.Name,
		Stdin:  strings.NewReader(req.script),
		Stdout: &memWriteCloser{buf: &out},
		Stderr: baseMS.Stderr,
		Log:    logger,
		Env:    env,
		Opts:   opts,
		PWD:    baseMS.PWD,
	}

	renderOpts := req.renderOpts
	_, _, err := compile(
		ctx,
		reqMS,
		s.localFiles,
		req.layout,
		&renderOpts,
		s.fontFamily,
		s.monoFontFamily,
		req.animateInterval,
		req.animateDuration,
		videoOptions{},
		"-",
		"-",
		req.boardPath,
		req.noChildren,
		req.bundle,
		req.forceAppendix,
		req.ext,
		req.asciiMode,
		nil,
	)
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func parseBoolQuery(q url.Values, name string) (bool, error) {
	v, err := strconv.ParseBool(q.Get(name))
	if err != nil {
		return false, badRequestError("invalid %s parameter %q", name, q.Get(name))
	}
	return v, nil
}

func parseInt64Query(q url.Values, name string) (int64, error) {
	v, err := strconv.ParseInt(q.Get(name), 10, 64)
	if err != nil {
		return 0, badRequestError("invalid %s parameter %q", name, q.Get(name))
	}
	return v, nil
}

func parseFloat64Query(q url.Values, name string) (float64, error) {
	v, err := strconv.ParseFloat(q.Get(name), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, badRequestError("invalid %s parameter %q", name, q.Get(name))
	}
	return v, nil
}

func classifyServeRenderError(err error) int {
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	var parseErr *d2parser.ParseError
	if errors.As(err, &parseErr) || errors.Is(err, localfile.ErrDenied) {
		return http.StatusBadRequest
	}
	var usageErr xmain.UsageError
	if errors.As(err, &usageErr) {
		return http.StatusBadRequest
	}
	msg := err.Error()
	for _, clientHint := range []string{
		"local-file access is disabled",
		"render target",
		"multiboard output cannot be written to stdout",
		"not a supported",
		"animate-interval",
	} {
		if strings.Contains(msg, clientHint) {
			return http.StatusBadRequest
		}
	}
	return http.StatusInternalServerError
}
