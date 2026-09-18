package compilerentry

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler/language"
	"github.com/d7z-team/mini-go/compiler/service"
)

func TestToolsWorkspaceSnapshot(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/language/workspace.json")
	if err != nil {
		t.Fatal(err)
	}
	var request ToolsRequest
	if err = json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	var tools ToolService
	request.Operation = "workspace/open"
	opened := tools.Execute(t.Context(), request)
	if opened.Error != nil {
		t.Fatal(opened.Error)
	}
	defer tools.Execute(t.Context(), ToolsRequest{Operation: "workspace/close", Session: opened.Session})
	analyzed := tools.Execute(t.Context(), ToolsRequest{Operation: "workspace/analyze", Session: opened.Session, Revision: opened.Revision})
	if analyzed.Error != nil || analyzed.Analysis == nil {
		t.Fatalf("analyze: %+v", analyzed)
	}
	hover := tools.Execute(t.Context(), ToolsRequest{Operation: "language/hover", Session: opened.Session, Query: service.Query{Snapshot: analyzed.Analysis.Snapshot, URI: "mini-go://sample/main.mgo", Position: language.Position{Line: 2, Character: 6}}})
	encoded, _ := json.Marshal(hover.Value)
	if hover.Error != nil || !strings.Contains(string(encoded), "Answer") {
		t.Fatalf("hover: %+v", hover)
	}
}
