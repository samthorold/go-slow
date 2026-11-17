package web

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
)

var (
	cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// cssBlock is a block of declarations; emptying them leaves the
	// selectors (and an @media's inner selectors) to read classes from.
	cssBlock    = regexp.MustCompile(`\{[^{}]*\}`)
	cssClass    = regexp.MustCompile(`\.([A-Za-z_][\w-]*)`)
	classAttr   = regexp.MustCompile(`class="([^"]*)"`)
	classAction = regexp.MustCompile(`\{\{.*?\}\}`)
	// dynamicClass is a class a template finishes from its data: a prefix
	// written in the attribute, its suffix an action.
	dynamicClass = regexp.MustCompile(`([\w-]+-)\{\{`)
	// jsClass is a class a static script names.
	jsClass = regexp.MustCompile(`classList\.(?:add|remove|toggle|contains)\(\s*["']([\w-]+)["']`)
)

// stylesheetClasses is every class go-slow.css defines a rule for.
func stylesheetClasses(t *testing.T) []string {
	t.Helper()
	css, err := fs.ReadFile(staticFS, "static/go-slow.css")
	if err != nil {
		t.Fatal(err)
	}
	selectors := cssComment.ReplaceAllString(string(css), "")
	for cssBlock.MatchString(selectors) {
		selectors = cssBlock.ReplaceAllString(selectors, "")
	}
	seen := map[string]bool{}
	var classes []string
	for _, m := range cssClass.FindAllStringSubmatch(selectors, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			classes = append(classes, m[1])
		}
	}
	sort.Strings(classes)
	return classes
}

// usedClasses is every class a template's markup or a static script can
// give an element. A class a template finishes from its data is listed by
// its every suffix: the status hues of a call's outcomes (outcome.html).
func usedClasses(t *testing.T) map[string]bool {
	t.Helper()
	suffixes := map[string][]string{"status-": nil}
	for _, o := range []editor.Outcome{editor.Applied, editor.Refused, editor.Failed} {
		suffixes["status-"] = append(suffixes["status-"], outcomeOf(o).Hue)
	}

	used := map[string]bool{}
	templates, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range templates {
		markup, err := fs.ReadFile(templateFS, name)
		if err != nil {
			t.Fatal(err)
		}
		for _, attr := range classAttr.FindAllStringSubmatch(string(markup), -1) {
			for _, d := range dynamicClass.FindAllStringSubmatch(attr[1], -1) {
				ends, ok := suffixes[d[1]]
				if !ok {
					t.Fatalf("%s finishes the class %q from its data; list its suffixes in this test", name, d[1])
				}
				for _, end := range ends {
					used[d[1]+end] = true
				}
			}
			for _, class := range strings.Fields(classAction.ReplaceAllString(attr[1], " ")) {
				if !strings.HasSuffix(class, "-") {
					used[class] = true
				}
			}
		}
	}

	scripts, err := fs.Glob(staticFS, "static/*.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range scripts {
		js, err := fs.ReadFile(staticFS, name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range jsClass.FindAllStringSubmatch(string(js), -1) {
			used[m[1]] = true
		}
	}
	return used
}

// The stylesheet styles only what is there: a rule for a class no template
// or script gives an element styles a Page or a control that is gone.
func TestEveryClassTheStylesheetDefinesIsUsed(t *testing.T) {
	classes := stylesheetClasses(t)
	if len(classes) == 0 {
		t.Fatal("found no classes in the stylesheet")
	}
	used := usedClasses(t)
	for _, class := range classes {
		if !used[class] {
			t.Errorf("go-slow.css defines .%s, which no template or static script uses", class)
		}
	}
}
