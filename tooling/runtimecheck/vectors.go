package runtimecheck

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/d7z-team/mini-go/compiler/types"
	"github.com/d7z-team/mini-go/runtime/bytecode"
)

func encodeJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// WireVector is a lossless canonical encoding observation from the Go backend.
type WireVector struct {
	Name      string          `json:"name"`
	Model     string          `json:"model"`
	Input     json.RawMessage `json:"input"`
	Canonical string          `json:"canonical"`
	Hash      string          `json:"hash"`
	Error     string          `json:"error,omitempty"`
}

// GenerateWireVectors covers zero structs, nil and empty collections, raw JSON,
// wide integers and escaping without relying on another serializer as oracle.
func GenerateWireVectors() ([]byte, error) {
	var vectors []WireVector
	for _, sample := range []struct {
		name, model string
		value       any
	}{
		{"zero_type", "TypeRef", types.TypeRef{}},
		{"primitive_type", "TypeRef", types.Builtin(types.PrimitiveUint64)},
		{"named_type", "TypeRef", types.TypeRef{Kind: types.Named, Named: types.TypeKey{ModulePath: "example/世界", DeclID: "Thing"}}},
		{"zero_node", "TypeNode", types.TypeNode{}},
		{"field_metadata", "TypeNode", types.TypeNode{ID: "n1", Kind: types.Struct, Fields: []types.Field{{Name: "世界", Type: types.Builtin(types.PrimitiveString), Tag: "json:\"<name>&\"\u2028\u2029", Embedded: true}}}},
		{"nil_image", "ExecutionImage", bytecode.ExecutionImage{}},
		{"empty_image", "ExecutionImage", bytecode.ExecutionImage{Entries: []bytecode.Entry{}, Packages: map[string]bytecode.PackageArchive{}}},
		{"raw_archive", "PackageArchive", bytecode.PackageArchive{Artifact: json.RawMessage(" { \"number\" : 18446744073709551615, \"text\" : \"<>&\u2028\u2029\" } "), ArtifactHash: "abc"}},
		{"ordered_packages", "ExecutionImage", bytecode.ExecutionImage{Packages: map[string]bytecode.PackageArchive{"z": {Artifact: json.RawMessage(`{"x":-0.0}`)}, "a": {Artifact: json.RawMessage(`{"x":1e+99}`)}}}},
		{"zero_symbols", "ProgramSymbols", bytecode.ProgramSymbols{}},
		{"raw_constant", "Constant", bytecode.Constant{ID: "huge", Type: types.Builtin(types.PrimitiveUint64), Value: json.RawMessage(`18446744073709551615`)}},
	} {
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(sample.value); err != nil {
			return nil, fmt.Errorf("vector %s: %w", sample.name, err)
		}
		canonical := bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
		digest := sha256.Sum256(canonical)
		vectors = append(vectors, WireVector{Name: sample.name, Model: sample.model, Input: append(json.RawMessage(nil), canonical...), Canonical: string(canonical), Hash: hex.EncodeToString(digest[:])})
	}
	for _, sample := range []struct {
		name, model, input string
		value              any
	}{
		{"duplicate_scalar", "TypeRef", `{"kind":3,"primitive":3,"primitive":5}`, &types.TypeRef{}},
		{"null_scalar_preserves_value", "TypeRef", `{"kind":3,"primitive":3,"primitive":null,"node":null}`, &types.TypeRef{}},
		{"duplicate_struct_merges_fields", "TypeRef", `{"named":{"module_path":"example"},"named":{"decl_id":"T"}}`, &types.TypeRef{}},
		{"field_case_folding", "TypeRef", `{"KIND":3,"Primitive":12}`, &types.TypeRef{}},
		{"unicode_field_case_folding", "TypeRef", `{"Kind":3,"primitive":3}`, &types.TypeRef{}},
		{"null_struct", "TypeRef", `null`, &types.TypeRef{}},
		{"duplicate_map_merges_keys", "ExecutionImage", `{"packages":{"a":{}},"packages":{"b":{}}}`, &bytecode.ExecutionImage{}},
		{"null_map_clears_keys", "ExecutionImage", `{"packages":{"a":{}},"packages":null}`, &bytecode.ExecutionImage{}},
		{"duplicate_pointer_merges_fields", "TypeNode", `{"signature":{"params":[{"type":{"kind":3,"primitive":3}}]},"signature":{"results":[{"kind":3,"primitive":2}]}}`, &types.TypeNode{}},
		{"raw_null_retains_presence", "Constant", `{"value":null}`, &bytecode.Constant{}},
		{"unknown_field", "TypeRef", `{"unknown":1}`, &types.TypeRef{}},
		{"invalid_numeric_type", "TypeRef", `{"primitive":true}`, &types.TypeRef{}},
	} {
		vector := WireVector{Name: sample.name, Model: sample.model, Input: json.RawMessage(sample.input)}
		decoder := json.NewDecoder(bytes.NewBufferString(sample.input))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(sample.value); err != nil {
			vector.Error = err.Error()
		} else {
			canonical, err := encodeJSON(sample.value)
			if err != nil {
				return nil, err
			}
			canonical = bytes.TrimSuffix(canonical, []byte("\n"))
			vector.Canonical = string(canonical)
			digest := sha256.Sum256(canonical)
			vector.Hash = hex.EncodeToString(digest[:])
		}
		vectors = append(vectors, vector)
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(vectors); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
