package gateway

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/d7z-team/mini-go/rpc/router"
)

func TestSharedCatalogIdentities(t *testing.T) {
	data, err := os.ReadFile("../../testdata/rpc/wire/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Bundle      router.MRPCBundle
		Publication PublicationSnapshot
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	bundle, err := router.NewMRPCBundle(fixture.Bundle.ImportPath, fixture.Bundle.Files)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bundle, fixture.Bundle) {
		t.Fatalf("bundle identity: %#v", bundle)
	}
	snapshot, err := normalizePublicationSnapshot(fixture.Publication)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ID != fixture.Publication.ID {
		t.Fatalf("snapshot ID %s", snapshot.ID)
	}
	fixture.Publication.Providers[0].Options.Labels["zone"] = "changed"
	if _, err := normalizePublicationSnapshot(fixture.Publication); err == nil {
		t.Fatal("changed publication accepted with previous identity")
	}
}
