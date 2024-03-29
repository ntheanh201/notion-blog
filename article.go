package main

import (
	"fmt"
	"github.com/mitchellh/mapstructure"
	"github.com/ntheanh201/blog/entity"
	"html/template"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kjk/notionapi"
)

// for Article.Status
const (
	statusPublished = iota // show on main page
	statusIdea
	statusDraft
	statusRevise
)

// URLPath describes
type URLPath struct {
	URL  string
	Name string
}

// MetaValue represents a key/value metadata
type MetaValue struct {
	key   string
	value string
}

// ImageMapping keeps track of rewritten image urls (locally cached
// images in notion)
type ImageMapping struct {
	// this is Block.Source from image block
	link string
	// this is path on the disk
	path string
	// this is relative url of the image on disk
	relativeURL string
}

type BlockInfo struct {
	// if true, this block should be skipped when generating html
	shouldSkip bool

	// for #url metadata, if image is supposed to be inside <a> tag
	// this is href for it
	imageURL string

	// for #gallery meta-data, this is a list of image urls
	// this is a path that needs to be looked up in Images to get relative URL
	galleryImages []string
}

// Article describes a single article
type Article struct {
	page         *notionapi.Page
	notionClient *notionapi.CachingClient

	ID                   string
	PublishedOn          time.Time
	UpdatedOn            time.Time
	Title                string
	Type                 string
	Tags                 []string
	Summary              string
	Slug                 string
	BodyHTML             string
	HTMLBody             template.HTML
	HeaderImageURL       string
	Collection           string
	CollectionURL        string
	Status               int
	Description          string
	Paths                []URLPath
	Metadata             []*MetaValue
	urlOverride          string
	publishedOnOverwrite time.Time

	// if true, this belongs to blog i.e. will be present in atom.xml
	// and listed in blog section
	inBlog bool

	UpdatedAgeStr string
	Images        []*ImageMapping

	blockInfos map[*notionapi.Block]*BlockInfo
}

// URL returns article's permalink
func (a *Article) URL() string {
	if a.urlOverride != "" {
		return a.urlOverride
	}
	// TODO-ntheanh201: this is where handle article's url
	return "/articles/" + urlify(a.Title) + ".html"
}

// PathAsText returns navigation path as text
func (a *Article) PathAsText() string {
	paths := []string{"Home"}
	for _, urlpath := range a.Paths {
		paths = append(paths, urlpath.Name)
	}
	return strings.Join(paths, " / ")
}

// TagsDisplay returns tags as html
func (a *Article) TagsDisplay() template.HTML {
	arr := make([]string, 0)
	for _, tag := range a.Tags {
		// TODO: url-quote the first tag
		escapedURL := fmt.Sprintf(`<a href="/tag/%s" class="taglink">%s</a>`, tag, tag)
		arr = append(arr, escapedURL)
	}
	s := strings.Join(arr, ", ")
	return template.HTML(s)
}

// PublishedOnShort is a short version of date
func (a *Article) PublishedOnShort() string {
	return a.PublishedOn.Format("Jan 2 2006")
}

// IsBlog returns true if this article belongs to a blog
func (a *Article) IsBlog() bool {
	return a.inBlog
}

// UpdatedAge returns when it was updated last, in days
func (a *Article) UpdatedAge() int {
	dur := time.Since(a.UpdatedOn)
	return int(dur / (time.Hour * 24))
}

// IsHidden returns true if article should not be shown in the index
func (a *Article) IsHidden() bool {
	return a.Status == statusIdea || a.Status == statusDraft || a.Status == statusRevise
}

func (a *Article) getBlockInfo(block *notionapi.Block) *BlockInfo {
	bi := a.blockInfos[block]
	if bi == nil {
		bi = &BlockInfo{}
		a.blockInfos[block] = bi
	}
	return bi
}

func (a *Article) markBlockToSkip(block *notionapi.Block) {
	a.getBlockInfo(block).shouldSkip = true
}

func (a *Article) shouldSkipBlock(block *notionapi.Block) bool {
	bi := a.blockInfos[block]
	if bi == nil {
		return false
	}
	return bi.shouldSkip
}

func (a *Article) setImageBlockURL(block *notionapi.Block, uri string) {
	a.getBlockInfo(block).imageURL = uri
}

func (a *Article) getImageBlockURL(block *notionapi.Block) string {
	bi := a.blockInfos[block]
	if bi == nil {
		return ""
	}
	return bi.imageURL
}

func (a *Article) setGalleryImages(block *notionapi.Block, imageURLS []string) {
	a.getBlockInfo(block).galleryImages = imageURLS
}

func (a *Article) getGalleryImages(block *notionapi.Block) []string {
	bi := a.blockInfos[block]
	if bi == nil {
		return nil
	}
	return bi.galleryImages
}

func (a *Article) removeEmptyTextBlocksAtEnd(root *notionapi.Block) {
	n := len(root.Content)
	blocks := root.Content
	for i := 0; i < n; i++ {
		idx := n - 1 - i
		block := blocks[idx]
		if !isEmptyTextBlock(block) {
			return
		}
		a.markBlockToSkip(block)
	}
}

func parseTags(s string) []string {
	tags := strings.Split(s, ",")
	var res []string
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		tag = strings.ToLower(tag)
		// skip the tag I use in quicknotes.io to tag notes for the blog
		if tag == "for-blog" || tag == "published" || tag == "draft" {
			continue
		}
		res = append(res, tag)
	}
	return res
}

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}
	t, err = time.Parse("2006-01-02", s)
	if err == nil {
		return t, nil
	}
	// TODO: more formats?
	return time.Now(), err
}

func parseStatus(status string) (int, error) {
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" || status == "Published" {
		return statusPublished, nil
	}
	switch status {
	case "Idea":
		return statusIdea, nil
	case "Draft":
		return statusDraft, nil
	case "Revise":
		return statusRevise, nil
	default:
		return 0, fmt.Errorf("'%s' is not a valid status", status)
	}
}

func isEmptyTextBlock(b *notionapi.Block) bool {
	if b.Type != notionapi.BlockText {
		return false
	}
	if len(b.InlineContent) > 0 {
		return false
	}
	return true
}

func (a *Article) SetID(v string) {
	// we handle 3 types of ids:
	// - blog posts from articles/ directory have integer id
	// - blog posts imported from quicknotes have id that are strings
	// - articles written in notion, have notion string id
	a.ID = strings.TrimSpace(v)
	id, err := strconv.Atoi(a.ID)
	if err == nil {
		a.ID = encodeBase64(id)
	}
}

func (a *Article) setStatusMust(val string) {
	var err error
	a.Status, err = parseStatus(val)
	must(err)
}

func (a *Article) setCollectionMust(val string) {
	collectionURL := ""
	switch val {
	case "go-cookbook":
		collectionURL = "/book/go-cookbook.html"
		val = "Go Cookbook"
	case "go-windows":
		// ignore
		return
	}
	panicIf(collectionURL == "", "'%s' is not a known collection", val)
	a.Collection = val
	a.CollectionURL = collectionURL

}

func (a *Article) setHeaderImageMust(val string) {
	if val[0] != '/' {
		val = "/" + val
	}
	path := filepath.Join("www", val)
	panicIf(!fileExists(path), "File '%s' for @header-image doesn't exist", path)
	//uri := getHostURL() + val
	// logf(ctx(), "Found HeaderImageURL: %s\n", uri)
	uri := val
	a.HeaderImageURL = uri
}

func getInlineBlocksText(blocks []*notionapi.TextSpan) string {
	s := ""
	for _, b := range blocks {
		s += b.Text
	}
	return s
}

// parse: `#gallery` followed by an image blocks
// returns true if block was this kind of block
func (a *Article) maybeParseGallery(block *notionapi.Block, nBlock int, blocks []*notionapi.Block) bool {
	if block.Type != notionapi.BlockText {
		return false
	}
	s := getInlineBlocksText(block.InlineContent)
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#gallery") {
		return false
	}

	var imageBlocks []*notionapi.Block
	for i := nBlock + 1; i < len(blocks); i++ {
		im := blocks[i]
		if im.Type != notionapi.BlockImage {
			break
		}
		imageBlocks = append(imageBlocks, im)
	}

	if len(imageBlocks) < 2 {
		logf(ctx(), "Found #gallery followed by %d image blocks (should be at least 2). Page id: %s, #gallery block id: %s\n", len(imageBlocks), a.page.ID, block.ID)
		return false
	}
	var urls []string
	for _, b := range imageBlocks {
		a.markBlockToSkip(b)
		urls = append(urls, b.Source)
	}
	a.setGalleryImages(block, urls)
	return true
}

// parse: `#url ${url}“ followed by an image block
// returns true if block was this kind of block
func (a *Article) maybeParseImageURL(block *notionapi.Block, nBlock int, blocks []*notionapi.Block) bool {
	if block.Type != notionapi.BlockText {
		return false
	}
	s := getInlineBlocksText(block.InlineContent)
	s = strings.TrimSpace(s)
	uri := strings.TrimPrefix(s, "#url")
	if uri == s {
		return false
	}
	uri = strings.TrimSpace(uri)
	nNextBlock := nBlock + 1
	if nNextBlock > len(blocks)-1 {
		return false
	}
	nextBlock := blocks[nNextBlock]
	if nextBlock.Type != notionapi.BlockImage {
		return false
	}
	a.markBlockToSkip(block)
	a.setImageBlockURL(nextBlock, uri)
	return true
}

func (a *Article) maybeParseMeta(nBlock int, block *notionapi.Block) bool {
	var err error

	if block.Type != notionapi.BlockText {
		logTemp("extractMetadata: ending look because block %d is of type %s\n", nBlock, block.Type)
		return false
	}

	if len(block.InlineContent) == 0 {
		logTemp("block %d of type %s and has no InlineContent\n", nBlock, block.Type)
		return true
	} else {
		logTemp("block %d has %d InlineContent\n", nBlock, len(block.InlineContent))
	}

	inline := block.InlineContent[0]
	// must be plain text
	if !inline.IsPlain() {
		logTemp("block: %d of type %s: inline has attributes\n", nBlock, block.Type)
		return false
	}

	// remove empty lines at the top
	s := strings.TrimSpace(inline.Text)
	if s == "" {
		logTemp("block: %d of type %s: inline.Text is empty\n", nBlock, block.Type)
		return true
	}
	logTemp("  %d %s '%s'\n", nBlock, block.Type, s)

	// parse generic metadata like "@foo: bar" or "@foo bar"
	if s[0] == '@' {
		s := s[1:]
		idx := strings.Index(s, ":")
		if idx == -1 {
			idx = strings.Index(s, " ")
		}
		key := s
		value := ""
		if idx != -1 {
			key = s[:idx]
			value = s[idx+1:]
		}
		meta := &MetaValue{
			key:   key,
			value: value,
		}
		a.Metadata = append(a.Metadata, meta)
		return true
	}

	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		logTemp("block: %d of type %s: inline.Text is not key/value. s='%s'\n", nBlock, block.Type, s)
		return false
	}
	key := strings.ToLower(strings.TrimSpace(parts[0]))
	fmt.Println("Key: ", key)
	val := strings.TrimSpace(parts[1])
	switch key {
	case "tags":
		a.Tags = parseTags(val)
		logTemp("Tags: %v\n", a.Tags)
	case "id":
		a.SetID(val)
		logTemp("ID: %s\n", a.ID)
	case "publishedon":
		// PublishedOn over-writes Date and CreatedAt
		a.publishedOnOverwrite, err = parseDate(val)
		must(err)
		a.inBlog = true
		logTemp("got publishedon")
	case "date", "createdat":
		a.PublishedOn, err = parseDate(val)
		must(err)
		a.inBlog = true
		logTemp("got date or createdat")
	case "updatedat":
		a.UpdatedOn, err = parseDate(val)
		must(err)
	case "status":
		a.setStatusMust(val)
	case "description":
		a.Description = val
		logTemp("Description: %s\n", a.Description)
	case "summary":
		a.Summary = val
	case "headerimage":
		a.setHeaderImageMust(val)
	case "collection":
		a.setCollectionMust(val)
	case "url":
		a.urlOverride = val
	default:
		// assume that unrecognized meta means this article doesn't have
		// proper meta tags. It might miss meta-tags that are badly named
		return false
		/*
			rmCached(page.ID)
			title := page.Page.Title
			panicMsg("Unsupported meta '%s' in notion page with id '%s', '%s'", key, normalizeID(page.ID), title)
		*/
	}
	return true
}

func (a *Article) processBlocks(blocks []*notionapi.Block) {
	parsingMeta := true
	for nBlock, block := range blocks {
		logTemp("  %d %s '%s'\n", nBlock, block.Type, block.Title)

		if parsingMeta {
			parsingMeta = a.maybeParseMeta(nBlock, block)
			if parsingMeta {
				a.markBlockToSkip(block)
				continue
			}
		}

		parsed := a.maybeParseImageURL(block, nBlock, blocks)
		if parsed {
			continue
		}
		parsed = a.maybeParseGallery(block, nBlock, blocks)
		if parsed {
			continue
		}

		if block.Type == notionapi.BlockImage {
			link := block.Source
			resp, err := a.notionClient.DownloadFile(link, block)
			if err != nil {
				logf(ctx(), "genImage: DownloadFile('%s') from page https://notion.so/%s failed with '%s'\n", link, normalizeID(a.page.ID), err)
				a.notionClient.DownloadFile(link, block)
				must(err)
			}
			if !resp.FromCache {
				logf(ctx(), "genImage: DownloadFile('%s') from page https://notion.so/%s\n", link, normalizeID(a.page.ID))
			}
			path := resp.CacheFilePath
			relURL := "/img/" + filepath.Base(path)
			im := &ImageMapping{
				link:        link,
				path:        path,
				relativeURL: relURL,
			}
			a.Images = append(a.Images, im)
			continue
		}

		if len(block.Content) > 0 {
			a.processBlocks(block.Content)
		}
	}
}

func findImageMapping(images []*ImageMapping, link string) *ImageMapping {
	for _, im := range images {
		if im.link == link {
			return im
		}
	}
	logf(ctx(), "Didn't find image with link '%s'\n", link)
	logf(ctx(), "Available images:\n")
	for _, im := range images {
		logf(ctx(), "  link: %s, relativeURL: %s, path: %s\n", im.link, im.relativeURL, im.path)
	}
	return nil
}

func notionPageToArticle(c *notionapi.CachingClient, page *notionapi.Page) *Article {
	//logf(ctx(), "extractMetadata: %s-%s, %d blocks\n", title, id, len(blocks))
	// metadata blocks are always at the beginning. They are TypeText blocks and
	// have only one plain string as content
	root := page.Root()
	title := root.Title
	id := normalizeID(root.ID)

	properties := page.Root().Properties
	var blockExtend map[string][][]string
	err := mapstructure.Decode(properties, &blockExtend)
	if err != nil {
		fmt.Println("error unmarshal block extend with properties")
	}

	//itemsMap := blockExtend.(map[string]interface{})
	itemsMap := blockExtend
	var item entity.BlockExtend
	// Loop through the Items; we're not interested in the key, just the values
	for key, v := range itemsMap {
		// Use type assertions to ensure that the value's a JSON object
		//fmt.Println("key: ", key, v)
		switch key {
		//case "NX\\Q":
		//	item.StartDate = v[0][0]
		case "`gQ~":
			item.Type = v[0][0]
		case "title":
			item.Title = v[0][0]
		case "d]hq":
			item.Slug = v[0][0]
		case "sD^m":
			item.Tags = v[0]
		case "f211bdc0-ee00-4186-9a7d-f68c055ec2ee":
			item.Status = v[0][0]
		}
		//fmt.Println("item: ", item)
	}

	a := &Article{
		page:         page,
		Title:        title,
		blockInfos:   map[*notionapi.Block]*BlockInfo{},
		notionClient: c,
		Tags:         item.Tags,
		Type:         item.Type,
	}

	switch item.Status {
	case "Published":
		a.Status = statusPublished
	case "Idea":
		a.Status = statusIdea
	case "Draft":
		a.Status = statusDraft
	case "Revise":
		a.Status = statusRevise
	}

	// allow debugging for specific pages
	if false && id == "623523b67e1548a0b525749d6921465c" {
		doTempLog = true
		defer func() {
			doTempLog = false
		}()
		logTemp("Temp logging article %s %s\n", id, title)
	}

	a.PublishedOn = root.CreatedOn()
	a.UpdatedOn = root.LastEditedOn()

	a.processBlocks(page.Root().Content)

	if !a.publishedOnOverwrite.IsZero() {
		a.PublishedOn = a.publishedOnOverwrite
	}

	if a.ID == "" {
		a.ID = id
	}

	if a.Collection != "" {
		path := URLPath{
			Name: a.Collection,
			URL:  a.CollectionURL,
		}
		a.Paths = append(a.Paths, path)
	}

	format := root.FormatPage()
	// set image header from cover page
	if a.HeaderImageURL == "" && format != nil && format.PageCover != "" {
		rsp, err := c.DownloadFile(format.PageCover, root)
		panicIf(err != nil, "downloading '%s' for page '%s' failed with '%s'", format.PageCover, root.ID, err)
		path := rsp.CacheFilePath
		relURL := "/img/" + filepath.Base(path)
		im := &ImageMapping{
			link:        a.HeaderImageURL,
			path:        path,
			relativeURL: relURL,
		}
		a.Images = append(a.Images, im)
		//uri := getHostURL() + relURL
		uri := relURL
		a.HeaderImageURL = uri
	}

	a.removeEmptyTextBlocksAtEnd(page.Root())
	return a
}
