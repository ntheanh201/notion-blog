package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/kjk/common/server"
)

var (
	allArticles *Articles
	allTagURLS  []string // first item is tag, second is its url
	articleURLS []string // the order is the same as allArticles.articles
)

func tryServeFile(uri string, dir string) func(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(uri, "/")
	path := filepath.Join(dir, name)
	send := func(w http.ResponseWriter, r *http.Request) {
		logf(ctx(), "tryServeFile: serving '%s' with '%s'\n", uri, path)
		serveFileMust(w, r, path)
	}
	if fileExists(path) {
		//logf(ctx(), "tryServeFile: will serve '%s' with '%s'\n", uri, path)
		return send
	}
	return nil
}

func serveImage(uri string) func(w http.ResponseWriter, r *http.Request) {
	uri = strings.TrimPrefix(uri, "/img/")
	dir := filepath.Join("notion_cache", "files")
	return tryServeFile(uri, dir)
}

func serveStart(w http.ResponseWriter, r *http.Request, uri string) {
	if r == nil {
		return
	}
	ct := mimeTypeFromFileName(uri)
	w.Header().Add("Content-Type", ct)
	w.WriteHeader(http.StatusOK) // 200
}

func serverGet(uri string) func(w http.ResponseWriter, r *http.Request) {
	//logf(ctx(), "serverGet: '%s'\n", uri)
	store := allArticles
	if strings.HasPrefix(uri, "/img/") {
		return serveImage(uri)
	}
	if serve := tryServeFile(uri, "www"); serve != nil {
		return serve
	}
	writeData := func(w http.ResponseWriter, d []byte, err error) {
		must(err)
		_, err = w.Write(d)
		must(err)
	}
	switch uri {
	case "/index.html":
		return func(w http.ResponseWriter, r *http.Request) {
			//logf(ctx(), "serverGet: will serve '%s' with '%s'\n", uri, "genIndex")
			serveStart(w, r, uri)
			genIndex(store, w)
		}
	case "/archives.html":
		return func(w http.ResponseWriter, r *http.Request) {
			//logf(ctx(), "serverGet: will serve '%s' with '%s'\n", uri, "writeArticlesArchiveForTag")
			serveStart(w, r, uri)
			writeArticlesArchiveForTag(store, "", w)
		}
	case "/changelog.html":
		return func(w http.ResponseWriter, r *http.Request) {
			//logf(ctx(), "serverGet: will serve '%s' with '%s'\n", uri, "genChangelog")
			serveStart(w, r, uri)
			genChangelog(store, w)
		}
	case "/sitemap.xml":
		return func(w http.ResponseWriter, r *http.Request) {
			//logf(ctx(), "serverGet: will serve '%s' with '%s'\n", uri, "genSiteMap")
			serveStart(w, r, uri)
			d, err := genSiteMap(store, "https://ntheanh201.vercel.app")
			writeData(w, d, err)
		}
	case "/atom.xml":
		return func(w http.ResponseWriter, r *http.Request) {
			//logf(ctx(), "serverGet: will serve '%s' with '%s'\n", uri, "genAtomXML")
			serveStart(w, r, uri)
			d, err := genAtomXML(store, true)
			writeData(w, d, err)
		}
	case "/atom-all.xml":
		return func(w http.ResponseWriter, r *http.Request) {
			//logf(ctx(), "serverGet: will serve '%s' with '%s'\n", uri, "genAtomXML")
			serveStart(w, r, uri)
			d, err := genAtomXML(store, false)
			writeData(w, d, err)
		}
	case "/404.html":
		return func(w http.ResponseWriter, r *http.Request) {
			//logf(ctx(), "serverGet: will serve '%s' with '%s'\n", uri, "gen404")
			serveStart(w, r, uri)
			gen404(store, w)
		}
	}

	n := len(articleURLS)
	//uriLC := strings.ToLower(uri)
	for i := 0; i < n; i++ {
		if uri == articleURLS[i] {
			article := allArticles.articles[i]
			return func(w http.ResponseWriter, r *http.Request) {
				//logf(ctx(), "serverGet: will serve '%s' with '%s'\n", uri, "genArticle")
				serveStart(w, r, uri)
				genArticle(article, w)
			}
		}
	}

	n = len(allTagURLS)
	for i := 0; i < n; i += 2 {
		tagURL := allTagURLS[i+1]
		if uri == tagURL {
			tag := allTagURLS[i]
			return func(w http.ResponseWriter, r *http.Request) {
				//logf(ctx(), "serverGet: will serve '%s' with '%s'\n", uri, "writeArticlesArchiveForTag")
				serveStart(w, r, uri)
				writeArticlesArchiveForTag(allArticles, tag, w)
			}
		}
	}
	return nil
}

func serverURLS() []string {
	files := []string{
		"/index.html",
		"/archives.html",
		"/changelog.html",
		"/sitemap.xml",
		"/atom.xml",
		"/atom-all.xml",
		"/404.html",
	}
	files = append(files, articleURLS...)
	n := len(allTagURLS)
	for i := 0; i < n; i += 2 {
		tagURL := allTagURLS[i+1]
		files = append(files, tagURL)
	}
	return files
}

func makeDynamicServer() *server.Server {
	loadTemplates()

	serveAll := server.NewDynamicHandler(serverGet, serverURLS)

	// TODO: filter out templates etc.
	serveWWW := server.NewDirHandler("www", "/", nil)
	serveNotionImages := server.NewDirHandler(filepath.Join("notion_cache", "files"), "/img", nil)

	server := &server.Server{
		Handlers:  []server.Handler{serveWWW, serveNotionImages, serveAll},
		Port:      httpPort,
		CleanURLS: true,
	}

	cc := getNotionCachingClient()
	allArticles = loadArticles(cc)
	logf(ctx(), "got %d articles\n", len(allArticles.articles))

	store := allArticles
	tags := map[string]struct{}{}
	for _, article := range store.getBlogNotHidden() {
		for _, tag := range article.Tags {
			tags[tag] = struct{}{}
		}
	}
	for tag := range tags {
		tagURL := "/tag/" + tag + ".html" // TODO: URL-escape?
		allTagURLS = append(allTagURLS, tag, tagURL)
	}
	for _, article := range store.articles {
		uri := article.URL()
		articleURLS = append(articleURLS, uri)
	}
	return server
}

func genHTMLServer(dir string) {
	os.RemoveAll(dirWwwGenerated)
	regenMd()
	srv := makeDynamicServer()
	nFiles := 0
	totalSize := int64(0)
	onWritten := func(path string, d []byte) {
		fsize := int64(len(d))
		totalSize += fsize
		sizeStr := formatSize(fsize)
		if nFiles%256 == 0 {
			logf(ctx(), "generateStatic: file %d '%s' of size %s\n", nFiles+1, path, sizeStr)
		}
		nFiles++
	}
	server.WriteServerFilesToDir(dirWwwGenerated, srv.Handlers, onWritten)
}

func runServerDev() {
	logf(ctx(), "runServerDev\n")

	srv := makeDynamicServer()

	closeHTTPLog := OpenHTTPLog("blog")
	defer closeHTTPLog()

	httpSrv := makeHTTPServer(srv)
	logf(ctx(), "Starting server on http://%s'\n", httpSrv.Addr)
	if isWindows() {
		openBrowser(fmt.Sprintf("http://%s", httpSrv.Addr))
	}
	err := httpSrv.ListenAndServe()
	logf(ctx(), "runServer: httpSrv.ListenAndServe() returned '%s'\n", err)
}

func runServerProd() {
	panicIf(!dirExists(dirWwwGenerated))
	h := server.NewDirHandler(dirWwwGenerated, "/", nil)
	logf(ctx(), "runServerProd starting, hasSpacesCreds: %v, %d urls\n", hasSpacesCreds(), len(h.URLS()))
	srv := &server.Server{
		Handlers:  []server.Handler{h},
		CleanURLS: true,
		Port:      httpPort,
	}

	closeHTTPLog := OpenHTTPLog("blog")
	defer closeHTTPLog()

	httpSrv := makeHTTPServer(srv)
	logf(ctx(), "Starting server on http://%s'\n", httpSrv.Addr)
	if isWindows() {
		openBrowser(fmt.Sprintf("http://%s", httpSrv.Addr))
	}
	err := httpSrv.ListenAndServe()
	logf(ctx(), "runServerProd: httpSrv.ListenAndServe() returned '%s'\n", err)
}

var (
	prefixRedirects []string
)

func makeHTTPServer(srv *server.Server) *http.Server {
	panicIf(srv == nil, "must provide srv")
	httpPort := 8080
	if srv.Port != 0 {
		httpPort = srv.Port
	}
	httpAddr := fmt.Sprintf(":%d", httpPort)
	if isWindows() {
		httpAddr = "localhost" + httpAddr
	}

	redirects := readRedirectsJSON()

	mainHandler := func(w http.ResponseWriter, r *http.Request) {
		//logf(ctx(), "mainHandler: '%s'\n", r.RequestURI)

		tryServeRedirect := func(uri string) bool {
			if server.TryServeBadClient(w, r, nil) {
				return true
			}

			// noticed those urls in logs
			if strings.HasPrefix(uri, "/ntheanh201.vercel.app/") {
				newURI := strings.TrimPrefix(uri, "/ntheanh201.vercel.app")
				ref := r.Header.Get("Referer")
				logf(ctx(), "redirecting '%s' => '%s', referer: '%s'\n", uri, newURI, ref)
				http.Redirect(w, r, newURI, http.StatusTemporaryRedirect)
				return true
			}

			if ri, ok := redirects[uri]; ok {
				http.Redirect(w, r, ri.URL, ri.Code)
				return true
			}

			for i := 0; i < len(prefixRedirects); i += 2 {
				prefix := prefixRedirects[i]
				if strings.HasPrefix(uri, prefix) {
					redirectURL := prefixRedirects[i+1]
					http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
					return true
				}
			}

			return tryServeArticleRedirect(srv, w, r)
		}

		uri := r.URL.Path

		if strings.HasPrefix(uri, "/cheatsheets/") {
			redirectURL := "https://referenceguide.dev" + strings.Replace(uri, "/cheatsheets/", "/cheatsheet/", -1)
			if uri == "/cheatsheets/" {
				redirectURL = "https://referenceguide.dev"
			}
			http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
			return
		}

		if strings.HasPrefix(uri, "/gitoembed") {
			if uri == "/gitoembed/widget" {
				handleGitOembedWidget(w, r)
				return
			}
			if uri == "/gitoembed/oembed" {
				handleGitOembedOembed(w, r)
				return
			}
			handleGitOembedIndex(w, r)
			return
		}

		serve, is404 := srv.FindHandler(uri)
		if serve != nil {
			if is404 {
				if tryServeRedirect(uri) {
					return
				}
			}
			serve(w, r)
			return
		}
		http.NotFound(w, r)
	}

	handlerWithMetrics := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := httpsnoop.CaptureMetrics(http.HandlerFunc(mainHandler), w, r)
		defer func() {
			if p := recover(); p != nil {
				logf(ctx(), "mainHandler: panicked with with %v\n", p)
				errStr := fmt.Sprintf("Error: %v", p)
				http.Error(w, errStr, http.StatusInternalServerError)
				return
			}
			logHTTPReq(r, m.Code, m.Written, m.Duration)
		}()
	})

	httpSrv := &http.Server{
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second, // introduced in Go 1.8
		Handler:      handlerWithMetrics,
	}
	httpSrv.Addr = httpAddr
	return httpSrv
}

// /article/h/first-release-of-dbhero---a-gui-database-client.html
// should match the file under /article/h/
func tryServeArticleRedirect(srv *server.Server, w http.ResponseWriter, r *http.Request) bool {
	uri := r.URL.Path
	// logf(ctx(), "tryServeArticleRedirect: '%s'\n", uri)
	if !strings.HasPrefix(uri, "/articles/") {
		return false
	}
	rest := strings.TrimPrefix(uri, "/articles/")
	idx := strings.Index(rest, "/")
	if idx == -1 {
		return false
	}
	uriPrefix := "/articles/" + rest[:idx+1]
	// logf(ctx(), "tryServeArticleRedirect: uriPrefix: '%s'\n", uriPrefix)
	for _, h := range srv.Handlers {
		for _, uri := range h.URLS() {
			if strings.HasPrefix(uri, uriPrefix) {
				http.Redirect(w, r, uri, http.StatusTemporaryRedirect)
				// logf(ctx(), "tryServeArticleRedirect: did redirect to: '%s'\n", uri)
				return true
			}
		}
	}
	return false
}
