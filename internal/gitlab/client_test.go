package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseMRURLParsesProjectAndIID(t *testing.T) {
	projectPath, mrIID, err := ParseMRURL("https://gitlab.example.com/platform/application/-/merge_requests/108")
	if err != nil {
		t.Fatalf("ParseMRURL() error = %v", err)
	}
	if projectPath != "platform/application" || mrIID != 108 {
		t.Fatalf("ParseMRURL() = %q, %d", projectPath, mrIID)
	}
}

func TestClassifyMRURLRejectsHostMismatchAndAllowsCaseDifference(t *testing.T) {
	client, err := NewClient("https://gitlab.example.com", "token", http.DefaultClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, _, ok, err := client.ClassifyMRURL("https://example.com/platform/application/-/merge_requests/108")
	if err != nil {
		t.Fatalf("ClassifyMRURL() error = %v", err)
	}
	if ok {
		t.Fatal("ClassifyMRURL() ok = true, want false")
	}

	_, _, ok, err = client.ClassifyMRURL("https://GITLAB.EXAMPLE.COM/platform/application/-/merge_requests/108")
	if err != nil {
		t.Fatalf("ClassifyMRURL() error = %v", err)
	}
	if !ok {
		t.Fatal("ClassifyMRURL() ok = false, want true")
	}
}

func TestFormatDiffTruncatesEachFile(t *testing.T) {
	longDiff := make([]byte, 3001)
	for i := range longDiff {
		longDiff[i] = 'a'
	}

	formatted, truncated := FormatDiff(&MRChanges{Changes: []Change{{NewPath: "main.go", Diff: string(longDiff)}}})
	if !truncated {
		t.Fatal("FormatDiff() truncated = false, want true")
	}
	if !stringsContains(formatted, "[diff truncated at 3000 characters]") {
		t.Fatalf("formatted diff missing truncation marker: %q", formatted)
	}
}

func TestGetMRChangesEscapesProjectPathOnce(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"changes":[]}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.GetMRChanges(context.Background(), "platform/application", 108)
	if err != nil {
		t.Fatalf("GetMRChanges() error = %v", err)
	}

	wantPath := "/api/v4/projects/platform%2Fapplication/merge_requests/108/changes"
	if gotPath != wantPath {
		t.Fatalf("request path = %q, want %q", gotPath, wantPath)
	}
}

func stringsContains(value, needle string) bool {
	return len(needle) == 0 || len(value) >= len(needle) && (value == needle || stringsContains(value[1:], needle) || value[:len(needle)] == needle)
}
