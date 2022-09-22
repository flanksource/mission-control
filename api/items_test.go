package api

import "testing"

var fixtures = []struct {
	Items   []string
	Item    string
	Matches bool
}{
	{[]string{"!b"}, "a", true},
	{[]string{"!b"}, "b", false},
	{[]string{"b"}, "b", true},
	{[]string{"b", "c"}, "c", true},
	{[]string{"!b", "*"}, "c", true},
	{[]string{}, "c", true},
	{[]string{"b", "c"}, "", false},
}

func TestItems(t *testing.T) {
	for _, f := range fixtures {
		items := Items(f.Items)
		if items.Contains(f.Item) != f.Matches {
			t.Errorf("Expected %s to match %s", f.Item, f.Items)
		}
	}
}
