package lspserver

import (
	"net/url"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

func uriToPath(uri string) (string, bool) {
	return uriToPathOS(uri, runtime.GOOS)
}

func uriToPathOS(uri, goos string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || !path.IsAbs(u.Path) {
		return "", false
	}
	p := path.Clean(u.Path)
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		p = "//" + u.Host + p
	}
	if goos == "windows" {
		if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
			p = p[1:]
		}
		return strings.ReplaceAll(p, "/", `\`), true
	}
	return p, true
}

func pathToURI(p string) string {
	p = filepath.ToSlash(p)
	u := url.URL{Scheme: "file"}
	switch {
	case strings.HasPrefix(p, "//"):
		host, share, _ := strings.Cut(p[2:], "/")
		u.Host, p = host, "/"+share
	case len(p) >= 2 && p[1] == ':':
		p = "/" + p
	}
	u.Path = p
	return u.String()
}
