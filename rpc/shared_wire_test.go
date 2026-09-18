package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"
)

func TestSharedValueWire(t *testing.T) {
	data, err := os.ReadFile("../testdata/rpc/wire/values.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name    string `json:"name"`
		Hex     string `json:"hex"`
		Invalid bool   `json:"invalid"`
		Values  []struct {
			Type  string `json:"type"`
			Kind  string `json:"kind"`
			Value string `json:"value"`
		} `json:"values"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			wire, err := hex.DecodeString(fixture.Hex)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeValues(wire, Limits{})
			if fixture.Invalid {
				if err == nil {
					t.Fatal("invalid payload accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			values := make([]Value, len(fixture.Values))
			for i, input := range fixture.Values {
				value := Value{Type: input.Type}
				switch input.Kind {
				case "nil":
				case "bool":
					value.Data, err = strconv.ParseBool(input.Value)
				case "int":
					value.Data, err = strconv.ParseInt(input.Value, 10, 64)
				case "uint":
					value.Data, err = strconv.ParseUint(input.Value, 10, 64)
				case "float-bits":
					var bits uint64
					bits, err = strconv.ParseUint(input.Value, 16, 64)
					value.Data = math.Float64frombits(bits)
				case "bytes":
					value.Data, err = hex.DecodeString(input.Value)
				case "string":
					value.Data = input.Value
				default:
					t.Fatalf("unknown fixture kind %q", input.Kind)
				}
				if err != nil {
					t.Fatal(err)
				}
				values[i] = value
			}
			for _, input := range [][]Value{values, decoded} {
				encoded, err := EncodeValues(input, Limits{})
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(encoded, wire) {
					t.Fatalf("wire = %x, want %x", encoded, wire)
				}
			}
		})
	}
}
