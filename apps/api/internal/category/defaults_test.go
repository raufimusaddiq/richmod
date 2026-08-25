package category

import "testing"

func TestIndonesianDefaultsHaveUniqueSlugsAndKnownParents(t *testing.T) {
	seen := make(map[string]bool, len(IndonesianDefaults))
	for _, category := range IndonesianDefaults {
		if category.Name == "" || category.Slug == "" {
			t.Fatalf("category must have name and slug: %#v", category)
		}
		if seen[category.Slug] {
			t.Fatalf("duplicate slug %q", category.Slug)
		}
		seen[category.Slug] = true
	}
	for _, category := range IndonesianDefaults {
		if category.ParentSlug != "" && !seen[category.ParentSlug] {
			t.Fatalf("unknown parent %q for %q", category.ParentSlug, category.Slug)
		}
	}
}
