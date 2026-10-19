package app

import "testing"

func TestParseOrigins(t *testing.T) {
	origins, err := parseOrigins("https://app.example, https://app.example/, http://127.0.0.1:5173", "production")
	if err != nil {
		t.Fatal(err)
	}
	if len(origins) != 2 {
		t.Fatalf("expected duplicate to be removed, got %#v", origins)
	}

	for _, raw := range []string{
		"*",
		"http://app.example",
		"https://user:pass@app.example",
		"https://app.example/path",
		"https://app.example?token=x",
		"null",
	} {
		if _, err := parseOrigins(raw, "production"); err == nil {
			t.Errorf("expected %q to be rejected", raw)
		}
	}
}
