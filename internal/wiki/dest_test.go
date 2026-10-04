package wiki

import "testing"

// A markdown destination written with an entity reference resolves, and a move
// rewrites it, so the cache and the page source agree on what it names.
func TestMoveRewritesAnEntityEncodedDestination(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{
		"no_such": "# No such\n",
		"index":   "[f](no&#95;such.md#n&#111;-such)\n",
	})
	plan, err := w.PlanMove("no_such", "other")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Edits) != 1 || plan.Edits[0].Old != "no&#95;such.md#n&#111;-such" || plan.Edits[0].New != "other.md#no-such" {
		t.Fatalf("plan edits = %+v, want the entity-encoded link rewritten", plan.Edits)
	}
}
