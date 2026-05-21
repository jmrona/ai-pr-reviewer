package jira

import (
	"net/http"
	"strings"
	"testing"
)

func TestParseTicketURLParsesIssueKey(t *testing.T) {
	issueKey, err := ParseTicketURL("https://jira.example.com/browse/PROJ-141")
	if err != nil {
		t.Fatalf("ParseTicketURL() error = %v", err)
	}
	if issueKey != "PROJ-141" {
		t.Fatalf("ParseTicketURL() = %q, want PROJ-141", issueKey)
	}
}

func TestClassifyTicketURLRejectsHostMismatchAndAllowsCaseDifference(t *testing.T) {
	client, err := NewClient("https://jira.example.com", "email", "token", http.DefaultClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, ok, err := client.ClassifyTicketURL("https://example.atlassian.net/browse/PROJ-141")
	if err != nil {
		t.Fatalf("ClassifyTicketURL() error = %v", err)
	}
	if ok {
		t.Fatal("ClassifyTicketURL() ok = true, want false")
	}

	_, ok, err = client.ClassifyTicketURL("https://JIRA.EXAMPLE.COM/browse/PROJ-141")
	if err != nil {
		t.Fatalf("ClassifyTicketURL() error = %v", err)
	}
	if !ok {
		t.Fatal("ClassifyTicketURL() ok = false, want true")
	}
}

func TestExtractADFTextTraversesKnownAndUnknownNodes(t *testing.T) {
	text := ExtractADFText(ADFNode{Type: "doc", Content: []ADFNode{
		{Type: "heading", Content: []ADFNode{{Type: "text", Text: "Acceptance Criteria"}}},
		{Type: "bulletList", Content: []ADFNode{
			{Type: "listItem", Content: []ADFNode{{Type: "paragraph", Content: []ADFNode{{Type: "text", Text: "Works offline"}}}}},
		}},
		{Type: "custom", Content: []ADFNode{{Type: "text", Text: "Unknown node text"}}},
	}})

	for _, want := range []string{"Acceptance Criteria", "Works offline", "Unknown node text"} {
		if !strings.Contains(text, want) {
			t.Fatalf("ExtractADFText() = %q, missing %q", text, want)
		}
	}
}
