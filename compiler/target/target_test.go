package target

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeTags(t *testing.T) {
	target, err := Normalize(Target{Tags: []string{"feature", "minigo", "feature", "debug"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"debug", "feature", "minigo"}; !reflect.DeepEqual(target.Tags, want) {
		t.Fatalf("tags = %#v, want %#v", target.Tags, want)
	}
	for _, invalid := range []string{"", "has space", "!debug", "a/b", "x-y"} {
		if _, err := Normalize(Target{Tags: []string{invalid}}); err == nil {
			t.Fatalf("tag %q was accepted", invalid)
		}
	}
}

func TestMatchSource(t *testing.T) {
	target := Target{Tags: []string{"debug", "net.test"}}
	for _, test := range []struct {
		expression string
		want       bool
	}{
		{"minigo", true},
		{"debug && minigo", true},
		{"release || net.test", true},
		{"debug && !release", true},
		{"(release || debug) && !net.test", false},
	} {
		source := "//go:build " + test.expression + "\n\npackage sample\n"
		got, err := MatchSource(source, target)
		if err != nil || got != test.want {
			t.Fatalf("MatchSource(%q) = %v, %v; want %v", test.expression, got, err, test.want)
		}
	}
	if got, err := MatchSource("package sample\n", target); err != nil || !got {
		t.Fatalf("unconstrained source = %v, %v", got, err)
	}
}

func TestMatchSourceRejectsMalformedConstraints(t *testing.T) {
	for _, source := range []string{
		"//go:build\npackage sample\n",
		"//go:build debug &&\npackage sample\n",
		"//go:build (debug\npackage sample\n",
		"//go:build debug\n//go:build release\npackage sample\n",
		"//go:build debug & release\npackage sample\n",
	} {
		_, err := MatchSource(source, Target{})
		var constraintErr ConstraintError
		if !errors.As(err, &constraintErr) {
			t.Fatalf("MatchSource(%q) error = %v", source, err)
		}
	}
}

func TestMatchSourceIgnoresSimilarComments(t *testing.T) {
	source := "//go:buildx debug\npackage sample\n"
	matched, err := MatchSource(source, Target{})
	if err != nil || !matched {
		t.Fatalf("similar comment matched as directive: %v, %v", matched, err)
	}
}

func TestMatchSourceAfterBlockComment(t *testing.T) {
	source := "/*\nCopyright\n*/\n//go:build debug\n\npackage sample\n"
	matched, err := MatchSource(source, Target{Tags: []string{"debug"}})
	if err != nil || !matched {
		t.Fatalf("directive after block comment = %v, %v", matched, err)
	}
}
