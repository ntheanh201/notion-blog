package main

import (
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"math/rand"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kjk/u"

	"github.com/chilts/sid"
	"github.com/kjk/betterguid"
	"github.com/oklog/ulid"
	"github.com/rs/xid"
	uuid "github.com/satori/go.uuid"
	"github.com/segmentio/ksuid"
	"github.com/sony/sonyflake"
	atom "github.com/thomas11/atomgenerator"
)

func addAllRedirects(store *Articles) {
	addStaticRedirects()
	//addRewrite("/book/", "/static/documents.html")
	//addTempRedirect("/book/*", "/article/:splat")
	addTempRedirect("/software/sumatrapdf*", "https://www.sumatrapdfreader.org/:splat")

	addTempRedirect("/articles/", "/documents.html")
	addTempRedirect("/articles/index.html", "/documents.html")
	addTempRedirect("/static/documents.html", "/documents.html")
	addTempRedirect("/software/index.html", "/software/")

	addRewrite("/articles/go-cookbook.html", "/book/go-cookbook.html")

	for _, article := range store.articles {
		if article.urlOverride != "" {
			path := fmt.Sprintf("/article/%s.html", article.ID)
			addRewrite(article.urlOverride, path)
		}
	}

	addArticleRedirects(store)
}

func copyAndSortArticles(articles []*Article) []*Article {
	n := len(articles)
	res := make([]*Article, n)
	copy(res, articles)
	sort.Slice(res, func(i, j int) bool {
		return res[j].PublishedOn.After(res[i].PublishedOn)
	})
	return res
}

func genAtomXML(store *Articles, excludeNotes bool) ([]byte, error) {
	articles := store.getBlogNotHidden()
	if excludeNotes {
		articles = filterArticlesByTag(articles, "note", false)
	}
	articles = copyAndSortArticles(articles)
	n := 25
	if n > len(articles) {
		n = len(articles)
	}

	latest := make([]*Article, n)
	size := len(articles)
	for i := 0; i < n; i++ {
		latest[i] = articles[size-1-i]
	}

	pubTime := time.Now()
	if len(articles) > 0 {
		pubTime = articles[0].PublishedOn
	}

	feed := &atom.Feed{
		Title:   "Krzysztof Kowalczyk blog",
		Link:    "https://blog.kowalczyk.info/atom.xml",
		PubDate: pubTime,
	}

	for _, a := range latest {
		//id := fmt.Sprintf("tag:blog.kowalczyk.info,1999:%d", a.Id)
		e := &atom.Entry{
			Title:   a.Title,
			Link:    "https://blog.kowalczyk.info" + a.URL(),
			Content: a.BodyHTML,
			PubDate: a.PublishedOn,
		}
		feed.AddEntry(e)
	}

	return feed.GenXml()
}

func wwwPath(fileName string) string {
	fileName = strings.TrimLeft(fileName, "/")
	path := filepath.Join(generatedHTMLDir, fileName)
	u.CreateDirForFileMust(path)
	return path
}

func wwwWriteFile(fileName string, d []byte) {
	path := wwwPath(fileName)
	//logf(ctx(), "%s\n", path)
	ioutil.WriteFile(path, d, 0644)
}

// TODO: should be https://blog.kjk.workers.dev for dev deployment
func getHostURL() string {
	return "https://blog.kowalczyk.info"
}

// https://www.linkedin.com/shareArticle?mini=true&;url=https://nodesource.com/blog/why-the-new-v8-is-so-damn-fast"
func makeLinkedinShareURL(article *Article) string {
	uri := getHostURL() + article.URL()
	uri = url.QueryEscape(uri)
	return fmt.Sprintf(`https://www.linkedin.com/shareArticle?mini=true&url=%s`, uri)
}

// https://www.facebook.com/sharer/sharer.php?u=https://nodesource.com/blog/why-the-new-v8-is-so-damn-fast
func makeFacebookShareURL(article *Article) string {
	uri := getHostURL() + article.URL()
	uri = url.QueryEscape(uri)
	return fmt.Sprintf(`https://www.facebook.com/sharer/sharer.php?u=%s`, uri)
}

// https://twitter.com/intent/tweet?text=%s&url=%s&via=kjk
func makeTwitterShareURL(article *Article) string {
	title := url.QueryEscape(article.Title)
	uri := getHostURL() + article.URL()
	uri = url.QueryEscape(uri)
	return fmt.Sprintf(`https://twitter.com/intent/tweet?text=%s&url=%s&via=kjk`, title, uri)
}

// TagInfo represents a single tag for articles
type TagInfo struct {
	URL   string
	Name  string
	Count int
}

var (
	allTags []*TagInfo
)

func buildTags(articles []*Article) []*TagInfo {
	if allTags != nil {
		return allTags
	}

	var res []*TagInfo
	ti := &TagInfo{
		URL:   "/archives.html",
		Name:  "all",
		Count: len(articles),
	}
	res = append(res, ti)

	tagCounts := make(map[string]int)
	for _, a := range articles {
		for _, tag := range a.Tags {
			tagCounts[tag]++
		}
	}
	var tags []string
	for tag := range tagCounts {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	for _, tag := range tags {
		count := tagCounts[tag]
		ti = &TagInfo{
			URL:   "/tag/" + tag,
			Name:  tag,
			Count: count,
		}
		res = append(res, ti)
	}
	allTags = res
	return res
}

func writeArticlesArchiveForTag(store *Articles, tag string, w io.Writer) error {
	path := "/archives.html"
	articles := store.getBlogNotHidden()
	if tag != "" {
		articles = filterArticlesByTag(articles, tag, true)
		// must manually resolve conflict due to urlify
		tagInPath := tag
		if tag == "c#" {
			tagInPath = "csharp"
		} else if tag == "c++" {
			tagInPath = "cplusplus"
		}
		tagInPath = urlify(tagInPath)
		path = fmt.Sprintf("/article/archives-by-tag-%s.html", tagInPath)
		from := "/tag/" + tag
		addRewrite(from, path)
	}

	model := struct {
		AnalyticsHTML template.HTML
		Article       *Article
		PostsCount    int
		Tag           string
		Years         []Year
		Tags          []*TagInfo
	}{
		AnalyticsHTML: analyticsHTML(),
		PostsCount:    len(articles),
		Years:         buildYearsFromArticles(articles),
		Tag:           tag,
		Tags:          buildTags(articles),
	}

	return execTemplate(path, "archive.tmpl.html", model, w)
}

func copyImages() {
	srcDir := filepath.Join("notion_cache", "files")
	dstDir := filepath.Join(generatedHTMLDir, "img")
	u.DirCopyRecurMust(dstDir, srcDir, nil)
}

func genIndex(store *Articles, w io.Writer) error {
	// /
	articles := store.getBlogNotHidden()
	if len(articles) > 5 {
		articles = articles[:5]
	}
	articleCount := len(articles)
	websiteIndexPage := store.idToArticle[notionWebsiteStartPage]
	model := struct {
		AnalyticsHTML template.HTML
		Article       *Article
		Articles      []*Article
		ArticleCount  int
		WebsiteHTML   template.HTML
	}{
		AnalyticsHTML: analyticsHTML(),
		Article:       nil, // always nil
		ArticleCount:  articleCount,
		Articles:      articles,
		WebsiteHTML:   websiteIndexPage.HTMLBody,
	}
	return execTemplate("/index.html", "mainpage.tmpl.html", model, w)
}

func genChangelog(store *Articles, w io.Writer) error {
	// /changelog.html
	var articles []*Article
	for _, a := range store.articles {
		if !a.IsHidden() {
			articles = append(articles, a)
		}
	}
	sort.Slice(articles, func(i, j int) bool {
		a1 := articles[i]
		a2 := articles[j]
		return a1.UpdatedOn.After(a2.UpdatedOn)
	})
	if len(articles) > 64 {
		articles = articles[:64]
	}
	prevAge := -1
	for _, a := range articles {
		age := a.UpdatedAge()
		if prevAge != age {
			a.UpdatedAgeStr = fmt.Sprintf("%d d", a.UpdatedAge())
		}
		prevAge = age
	}

	model := struct {
		AnalyticsHTML template.HTML
		Article       *Article
		Articles      []*Article
	}{
		AnalyticsHTML: analyticsHTML(),
		Article:       nil, // always nil
		Articles:      articles,
	}
	return execTemplate("/changelog.html", "changelog.tmpl.html", model, w)
}

func gen404(store *Articles, w io.Writer) error {
	// store is not used
	model := struct {
		AnalyticsHTML template.HTML
	}{
		AnalyticsHTML: analytics404HTML(),
	}
	return execTemplate("/404.html", "404.tmpl.html", model, w)
}

func genPerTagArchives(store *Articles) {
	// tag/<tagname>
	tags := map[string]struct{}{}
	for _, article := range store.getBlogNotHidden() {
		for _, tag := range article.Tags {
			tags[tag] = struct{}{}
		}
	}
	for tag := range tags {
		writeArticlesArchiveForTag(store, tag, nil)
	}
}

func genArchives(store *Articles, w io.Writer) error {
	// /archives.html
	return writeArticlesArchiveForTag(store, "", w)
}

func writeFileOrWriter(path string, data []byte, w io.Writer) error {
	if w != nil {
		_, err := w.Write(data)
		return err
	}
	wwwWriteFile(path, data)
	return nil
}

func genSitemap(store *Articles, w io.Writer) error {
	// /sitemap.xml
	data, err := genSiteMap(store, "https://blog.kowalczyk.info")
	must(err)
	return writeFileOrWriter("/sitemap.xml", data, w)
}

func genAtom(store *Articles, w io.Writer) error {
	// /atom.xml
	d, err := genAtomXML(store, true)
	must(err)
	return writeFileOrWriter("/atom.xml", d, w)
}

func genAtomAll(store *Articles, w io.Writer) error {
	// /atom-all.xml
	d, err := genAtomXML(store, false)
	must(err)
	return writeFileOrWriter("/atom-all.xml", d, w)
}

func genArticle(article *Article, w io.Writer) error {
	canonicalURL := getHostURL() + article.URL()
	model := struct {
		AnalyticsHTML    template.HTML
		Article          *Article
		CanonicalURL     string
		CoverImage       string
		PageTitle        string
		TagsDisplay      string
		HeaderImageURL   string
		NotionEditURL    string
		Description      string
		TwitterShareURL  string
		FacebookShareURL string
		LinkedInShareURL string
	}{
		AnalyticsHTML:    analyticsHTML(),
		Article:          article,
		CanonicalURL:     canonicalURL,
		CoverImage:       article.HeaderImageURL,
		PageTitle:        article.Title,
		Description:      article.Description,
		TwitterShareURL:  makeTwitterShareURL(article),
		FacebookShareURL: makeFacebookShareURL(article),
		LinkedInShareURL: makeLinkedinShareURL(article),
	}
	if article.page != nil {
		id := normalizeID(article.page.ID)
		model.NotionEditURL = "https://notion.so/" + id
	}
	path := fmt.Sprintf("/article/%s.html", article.ID)
	logvf("%s => %s, %s, %s\n", article.ID, path, article.URL(), article.Title)
	return execTemplate(path, "article.tmpl.html", model, w)
}

func genGoCookbook(store *Articles, w io.Writer) error {
	// url: /book/go-cookbook.html
	model := struct {
		AnalyticsHTML template.HTML
	}{
		AnalyticsHTML: analyticsHTML(),
	}
	return execTemplate("/book/go-cookbook.html", "go-cookbook.tmpl.html", model, w)
}

/*
func genWindowsProgramming(store *Articles, w io.Writer) error {
	// url: /book/windows-programming-in-go.html
	model := struct {
	}{}
	return execTemplate("/book/go-cookbook.html", tmplGoC"go-cookbook.tmpl.html"ookBook, model, w)
}
*/

func genToolGenerateUniqueID(store *Articles, w io.Writer) error {
	// /tools/generate-unique-id
	idXid := xid.New()
	idKsuid := ksuid.New()

	t := time.Now().UTC()
	entropy := rand.New(rand.NewSource(t.UnixNano()))
	idUlid := ulid.MustNew(ulid.Timestamp(t), entropy)
	betterGUID := betterguid.New()
	uuid := uuid.NewV4()

	flake := sonyflake.NewSonyflake(sonyflake.Settings{})
	sfid, err := flake.NextID()
	sfidstr := fmt.Sprintf("%x", sfid)
	if err != nil {
		sfidstr = err.Error()
	}

	model := struct {
		AnalyticsHTML template.HTML
		Xid           string
		Ksuid         string
		Ulid          string
		BetterGUID    string
		Sonyflake     string
		Sid           string
		UUIDv4        string
	}{
		AnalyticsHTML: analyticsHTML(),
		Xid:           idXid.String(),
		Ksuid:         idKsuid.String(),
		Ulid:          idUlid.String(),
		BetterGUID:    betterGUID,
		Sonyflake:     sfidstr,
		Sid:           sid.Id(),
		UUIDv4:        uuid.String(),
	}

	// make sure /tools/generate-unique-id is served as html
	path := "/tools/generate-unique-id.html"
	addRewrite("/tools/generate-unique-id", path)
	return execTemplate(path, "generate-unique-id.tmpl.html", model, w)
}

func generateHTML(store *Articles) {
	outDir := generatedHTMLDir
	logf(ctx(), "generateHTML: %d articles in directory %s\n", len(store.articles), outDir)
	recreateDir(outDir)

	skipTmplFiles := func(path string) bool {
		return !strings.Contains(path, ".tmpl.")
	}

	copied := u.DirCopyRecurMust(outDir, "www", skipTmplFiles)
	logf(ctx(), "Copied %d files\n", len(copied))

	addAllRedirects(store)

	copyImages()

	genIndex(store, nil)

	genGoCookbook(store, nil)
	// genWindowsProgramming(store, nil)

	genChangelog(store, nil)

	genAtom(store, nil)
	genAtomAll(store, nil)

	{
		// /blog/ and /kb/ are only for redirects, we only handle /article/ at this point
		logvf("%d articles\n", len(store.idToPage))
		for _, article := range store.articles {
			genArticle(article, nil)
		}
	}

	genArchives(store, nil)
	genPerTagArchives(store)

	genSitemap(store, nil)
	gen404(nil, nil)

	genToolGenerateUniqueID(store, nil)

	// /ping
	wwwWriteFile("/ping", []byte("pong"))

	// no longer care about /worklog

	writeRedirects()
}
