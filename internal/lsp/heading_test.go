package lsp

import "testing"

// An empty heading has no text or slug to insert, so it is not offered.
func TestHeadingCompletionSkipsEmptyHeadings(t *testing.T) {
	c := newClient(t)
	uri := c.pageURI("index")
	c.open(uri, "")
	c.diagnostics(uri)

	for _, src := range []string{"#\n\n## Fresh\n\n##\n\n[x](#", "#\n\n## Fresh\n\n##\n\n[[#"} {
		items := c.complete(uri, src, 6, len(src)-len("#\n\n## Fresh\n\n##\n\n"))
		if len(items) != 1 || items[0].Label != "Fresh" {
			t.Errorf("complete(%q) = %+v, want only Fresh", src, items)
		}
	}
}
