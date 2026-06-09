package html

import (
	"strings"
	"testing"
)

func TestPrettyHTMLSourceIndentsNestedMarkup(t *testing.T) {
	got := prettyHTMLSource(`<div><h1 class="title">Hello</h1><p>World</p></div>`)
	want := strings.Join([]string{
		`<div>`,
		`  <h1 class="title">`,
		`    Hello`,
		`  </h1>`,
		`  <p>`,
		`    World`,
		`  </p>`,
		`</div>`,
	}, "\n")
	if got != want {
		t.Fatalf("unexpected pretty HTML:\n%s", got)
	}
}

func TestHTMLSourceEscapesAndHighlightsMarkup(t *testing.T) {
	got := string(PrettyHTMLSource(`<img src=x onerror="alert(1)">`))
	if !strings.Contains(got, `&lt;`) || !strings.Contains(got, `class="html-tag-name"`) {
		t.Fatalf("expected escaped highlighted HTML, got %s", got)
	}
	if strings.Contains(got, `<img`) || strings.Contains(got, `onerror="alert(1)"`) {
		t.Fatalf("source HTML was not safely escaped: %s", got)
	}
}
