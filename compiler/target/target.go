// Package target defines host-neutral Mini-Go build tags.
package target

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const LanguageTag = "minigo"

// Target is the canonical set of build tags used to select Mini-Go source files.
type Target struct {
	Tags []string `json:"tags"`
}

// Normalize validates, sorts, and deduplicates tags and adds the Mini-Go language tag.
func Normalize(buildTarget Target) (Target, error) {
	seen := map[string]struct{}{LanguageTag: {}}
	for _, tag := range buildTarget.Tags {
		tag = strings.TrimSpace(tag)
		if !validTag(tag) {
			return Target{}, fmt.Errorf("invalid build tag %q", tag)
		}
		seen[tag] = struct{}{}
	}
	tags := make([]string, 0, len(seen))
	for tag := range seen {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return Target{Tags: tags}, nil
}

// Validate reports whether a target is already in canonical form.
func Validate(buildTarget Target) error {
	normalized, err := Normalize(buildTarget)
	if err != nil {
		return err
	}
	if len(buildTarget.Tags) != len(normalized.Tags) {
		return errors.New("build tags are not canonical")
	}
	for i := range buildTarget.Tags {
		if buildTarget.Tags[i] != normalized.Tags[i] {
			return errors.New("build tags are not canonical")
		}
	}
	return nil
}

// Has reports whether the target contains a build tag.
func (t Target) Has(tag string) bool {
	for _, candidate := range t.Tags {
		if candidate == tag {
			return true
		}
	}
	return tag == LanguageTag
}

// Equal reports whether two targets normalize to the same build tags.
func (t Target) Equal(other Target) bool {
	left, leftErr := Normalize(t)
	right, rightErr := Normalize(other)
	if leftErr != nil || rightErr != nil || len(left.Tags) != len(right.Tags) {
		return false
	}
	for i := range left.Tags {
		if left.Tags[i] != right.Tags[i] {
			return false
		}
	}
	return true
}

func validTag(tag string) bool {
	if tag == "" {
		return false
	}
	for i := 0; i < len(tag); i++ {
		if !validTagByte(tag[i]) {
			return false
		}
	}
	return true
}
