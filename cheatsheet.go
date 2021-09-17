package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/aymerick/raymond"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
)

const csDir = "cheatsheets"

var (
	limitCheatsheets = false

	whitelistDevhings = []string{}
	whitelistOther    = []string{"python3"} // {"go", "python3"}

	blacklist = []string{"101", "absinthe", "analytics.js", "analytics", "angularjs", "appcache", "cheatsheet-styles", "deku@1", "enzyme@2", "figlet", "firefox", "go", "index", "index@2016", "ledger-csv", "ledger-examples", "ledger-format", "ledger-periods",
		"ledger-query", "ledger", "package", "phoenix-ecto@1.2", "phoenix-ecto@1.3", "phoenix@1.2", "python", "react@0.14", "README", "vue@1.0.28"}
)

func init() {
	if !limitCheatsheets {
		whitelistDevhings = nil
		whitelistOther = nil
	}
}

func newCsMarkdownParser() *parser.Parser {
	extensions := parser.NoIntraEmphasis |
		parser.Tables |
		parser.FencedCode |
		parser.Autolink |
		parser.Strikethrough |
		parser.SpaceHeadings |
		parser.AutoHeadingIDs |
		parser.HeadingIDs |
		parser.NoEmptyLineBeforeBlock
	return parser.NewWithExtensions(extensions)
}

func csBuildToc(md []byte, path string) []*tocNode {
	//logf("csBuildToc: %s\n", path)
	parser := newCsMarkdownParser()
	doc := parser.Parse(md)
	//ast.t(os.Stdout, doc)

	taken := map[string]bool{}
	ensureUniqueID := func(id string) {
		panicIf(taken[id], "duplicate heading id '%s' in '%s'", id, path)
		taken[id] = true
	}

	var currHeading *ast.Heading
	var currHeadingContent string
	var allHeaders []*tocNode
	var currToc *tocNode
	ast.WalkFunc(doc, func(node ast.Node, entering bool) ast.WalkStatus {
		switch v := node.(type) {
		case *ast.Heading:
			if entering {
				currHeading = v
			} else {
				ensureUniqueID(currHeading.HeadingID)
				tn := &tocNode{
					heading: currHeading,
					Content: currHeadingContent,
					ID:      currHeading.HeadingID,
					Level:   currHeading.Level,
				}
				allHeaders = append(allHeaders, tn)
				currToc = tn
				currHeading = nil
				currHeadingContent = ""
				//headingLevel := currHeading.Level
			}
		case *ast.Text:
			// the only child of ast.Heading is ast.Text (I think)
			if currHeading != nil && entering {
				currHeadingContent = string(v.Literal)
			} else {
				if entering && currToc != nil {
					currToc.SiblingsCount++
				}
			}
		default:
			if entering && currToc != nil {
				currToc.SiblingsCount++
			}
		}
		return ast.GoToNext
	})

	if false {
		for _, tn := range allHeaders {
			logf("h%d #%s %s %d siblings\n", tn.Level, tn.heading.HeadingID, tn.Content, tn.SiblingsCount)
		}
	}
	cloneNode := func(n *tocNode) *tocNode {
		// clone but without children
		return &tocNode{
			heading:       n.heading,
			Content:       n.Content,
			Level:         n.Level,
			ID:            n.ID,
			SiblingsCount: n.SiblingsCount,
		}
	}

	buildToc := func() []*tocNode {
		first := cloneNode(allHeaders[0])
		toc := []*tocNode{first}
		stack := []*tocNode{first}
		for _, node := range allHeaders[1:] {
			node = cloneNode(node)
			stackLastIdx := len(stack) - 1
			curr := stack[stackLastIdx]
			currLevel := curr.Level
			nodeLevel := node.Level
			if nodeLevel > currLevel {
				// this is a child
				// TODO: should synthesize if we skip more than 1 level?
				curr.Children = append(curr.Children, node)
				stack = append(stack, node)
				curr = node
			} else if nodeLevel == currLevel {
				// this is a sibling, make current and attach to
				stack[stackLastIdx] = node
				if stackLastIdx > 0 {
					parent := stack[stackLastIdx-1]
					parent.Children = append(parent.Children, node)
				} else {
					toc = append(toc, node)
				}
			} else {
				// nodeLevel < currLevel
				for stackLastIdx > 0 {
					if stackLastIdx == 1 {
						toc = append(toc, node)
						stack = []*tocNode{node}
						stackLastIdx = 0
					} else {
						stack = stack[:stackLastIdx]
						stackLastIdx--
						curr = stack[stackLastIdx]
						if curr.Level == nodeLevel {
							stack[stackLastIdx] = node
							parent := stack[stackLastIdx-1]
							parent.Children = append(parent.Children, node)
							stackLastIdx = 0
						}
					}
				}
			}
		}
		return toc
	}
	toc := buildToc()
	if false {
		printToc(toc, 0)
	}
	return toc
}

func printToc(nodes []*tocNode, indent int) {
	indentStr := func(indent int) string {
		return "............................"[:indent]
	}
	hdrStr := func(level int) string {
		return "#################"[:level]
	}

	for _, n := range nodes {
		s := indentStr(indent)
		hdr := hdrStr(n.Level)
		logf("%s%s %s\n", s, hdr, n.Content)
		printToc(n.Children, indent+1)
	}
}

var reg *regexp.Regexp

func init() {
	reg = regexp.MustCompile(`{:.*}`)
}

func cleanupMarkdown(md []byte) []byte {
	s := string(md)
	// TODO: implement support of this in markdown parser
	// remove lines like: {: data-line="1"}
	s = reg.ReplaceAllString(s, "")
	s = strings.Replace(s, "{% raw %}", "", -1)
	s = strings.Replace(s, "{% endraw %}", "", -1)
	prev := s
	for prev != s {
		prev = s
		s = strings.Replace(s, "\n\n", "\n", -1)
	}
	return []byte(s)
}

type cheatSheet struct {
	fileNameBase string // unique name from file name, without extension
	mdFileName   string // path relative to www/cheatsheets directory
	mdPath       string
	htmlFullPath string
	// TODO: rename htmlFileName
	PathHTML   string // path relative to www/cheatsheets directory
	mdWithMeta []byte
	md         []byte
	meta       map[string]string
	html       []byte
	Title      string
}

func extractCheatSheetMetadata(cs *cheatSheet) {
	md := normalizeNewlines(cs.mdWithMeta)
	lines := strings.Split(string(md), "\n")
	// skip empty lines at the beginning
	for len(lines[0]) == 0 {
		lines = lines[1:]
	}
	if lines[0] != "---" {
		// no metadata
		cs.md = []byte(strings.Join(lines, "\n"))
		return
	}
	metaLines := []string{}
	lines = lines[1:]
	for lines[0] != "---" {
		metaLines = append(metaLines, lines[0])
		lines = lines[1:]
	}
	lines = lines[1:]
	cs.md = []byte(strings.Join(lines, "\n"))
	//logf("meta for '%s':\n%s\n", cs.mdPath, strings.Join(metaLines, "\n"))
	lastName := ""
	for _, line := range metaLines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 1 {
			s := strings.TrimSpace(parts[0])
			s = strings.Trim(s, `"`)
			v := cs.meta[lastName]
			if len(v) > 0 {
				v = v + "\n"
			}
			v += s
			cs.meta[lastName] = v
		} else {
			name := parts[0]
			s := strings.TrimSpace(parts[1])
			s = strings.Trim(s, `"`)
			s = strings.TrimLeft(s, "|")
			cs.meta[name] = s
			lastName = name
		}
	}
	cs.Title = cs.meta["title"]
	if cs.Title == "" {
		cs.Title = cs.fileNameBase
	}
}

type tocNode struct {
	heading *ast.Heading // not set if synthesized

	Content string
	Level   int
	ID      string

	SiblingsCount int

	Children []*tocNode // level of child is > our level
}

func processCheatSheet(cs *cheatSheet) {
	//logf("processCheatSheet: '%s'\n", cs.mdPath)
	cs.mdWithMeta = readFileMust(cs.mdPath)
	extractCheatSheetMetadata(cs)

	//logf("csGenHTML: for '%s'\n", cs.mdPath)
	md := cleanupMarkdown(cs.md)
	parser := newCsMarkdownParser()
	renderer := newMarkdownHTMLRenderer("")
	content := string(markdown.ToHTML(md, parser, renderer))
	toc := csBuildToc(md, cs.mdPath)
	tpl := string(readFileMust(filepath.Join(csDir, "cheatsheet.tmpl.html")))

	// on windows mdFileName is a windows-style path so change to unix/url style
	mdFileName := strings.Replace(cs.mdFileName, `\`, "/", -1)

	ctx := map[string]interface{}{
		"toc":        toc,
		"title":      cs.Title,
		"mdFileName": mdFileName,
		"content":    content,
	}
	cs.html = []byte(raymond.MustRender(tpl, ctx))

	//logf("Processed %s, html size: %d\n", cs.mdPath, len(cs.html))
}

func genIndexHTML(cheatsheets []*cheatSheet) string {
	// sort by title
	sort.Slice(cheatsheets, func(i, j int) bool {
		t1 := strings.ToLower(cheatsheets[i].Title)
		t2 := strings.ToLower(cheatsheets[j].Title)
		return t1 < t2
	})

	byCat := map[string][]*cheatSheet{}
	for _, cs := range cheatsheets {
		cat := cs.meta["category"]
		if cat == "" {
			continue
		}
		byCat[cat] = append(byCat[cat], cs)
	}

	// build toc for categories
	categories := []string{}
	for cat := range byCat {
		categories = append(categories, cat)
	}
	sort.Strings(categories)
	cats := []map[string]interface{}{}
	for _, category := range categories {
		v := map[string]interface{}{}
		v["category"] = category
		catMetas := byCat[category]
		v["cheatsheets"] = catMetas
		cats = append(cats, v)
	}

	tpl := string(readFileMust(filepath.Join(csDir, "index.tmpl.html")))
	ctx := map[string]interface{}{
		"cheatsheets":      cheatsheets,
		"CheatsheetsCount": len(cheatsheets),
		"categories":       cats,
	}
	s := raymond.MustRender(tpl, ctx)
	return s
}

func genCheatSheetFiles() map[string][]byte {
	cheatsheets := []*cheatSheet{}

	isBlacklisted := func(s string, a []string) bool {
		s = strings.ToLower(s)
		for _, s2 := range a {
			if s == strings.ToLower(s2) {
				return true
			}
		}
		return false
	}

	isWhitelisted := func(s string, a []string) bool {
		if a == nil {
			return true
		}
		s = strings.ToLower(s)
		for _, s2 := range a {
			if s == strings.ToLower(s2) {
				return true
			}
		}
		return false
	}

	readFromDir := func(subDir string, blacklist []string, whitelist []string) {
		dir := filepath.Join(csDir, subDir)
		files, err := os.ReadDir(dir)
		must(err)
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			name := f.Name()
			if filepath.Ext(name) != ".md" {
				continue
			}
			baseName := strings.Split(name, ".")[0]
			if isBlacklisted(baseName, blacklist) {
				//logf("blacklisted %s\n", f.Name())
				continue
			}
			if !isWhitelisted(baseName, whitelist) {
				//logf("!whitelisted %s\n", f.Name())
				continue
			}
			cs := &cheatSheet{
				fileNameBase: baseName,
				mdPath:       filepath.Join(dir, name),
				mdFileName:   filepath.Join(subDir, name),
				meta:         map[string]string{},
			}
			//logf("%s\n", cs.mdPath)
			cheatsheets = append(cheatsheets, cs)
		}
	}

	readFromDir("devhints", blacklist, whitelistDevhings)
	readFromDir("other", []string{"101v2"}, whitelistOther)

	{
		// uniquify names
		taken := map[string]bool{}
		for _, cs := range cheatsheets {
			name := cs.fileNameBase
			n := 0
			for taken[name] {
				n++
				name = fmt.Sprintf("%s%d", cs.fileNameBase, n)
			}
			taken[name] = true
			cs.fileNameBase = name
		}
	}

	for _, cs := range cheatsheets {
		cs.PathHTML = cs.fileNameBase + ".html"
		cs.htmlFullPath = filepath.Join(csDir, cs.PathHTML)
	}

	logf("%d cheatsheets\n", len(cheatsheets))

	nThreads := runtime.NumCPU()
	//nThreads := 1
	sem := make(chan bool, nThreads)
	var wg sync.WaitGroup
	for _, cs := range cheatsheets {
		wg.Add(1)
		sem <- true
		go func(cs *cheatSheet) {
			processCheatSheet(cs)
			wg.Done()
			<-sem
		}(cs)
	}
	wg.Wait()
	files := map[string][]byte{}
	{
		path := filepath.Join(csDir, "cheatsheet.js")
		name := filepath.Join("s", "cheatsheet.js")
		files[name] = readFileMust(path)
	}
	{
		path := filepath.Join(csDir, "cheatsheet.css")
		name := filepath.Join("s", "cheatsheet.css")
		files[name] = readFileMust(path)
	}
	for _, cs := range cheatsheets {
		d := cs.html
		name := filepath.Base(cs.htmlFullPath)
		files[name] = d
	}
	files["index.html"] = []byte(genIndexHTML(cheatsheets))
	return files
}

func previewCheatSheets() {
	files := genCheatSheetFiles()
	uri := uploadFilesToInstantPreviewMust(files)
	openBrowser(uri)
}

func genCheatSheets(outDir string) {
	files := genCheatSheetFiles()
	for fileName, d := range files {
		path := filepath.Join("cheatsheets", fileName)
		wwwWriteFile(path, d)
	}
}
