package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestSharedEnvelopeWire(t *testing.T) {
	data, err := os.ReadFile("../testdata/rpc/wire/envelopes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Kind, Hex string
		Invalid         bool
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			wire, err := hex.DecodeString(fixture.Hex)
			if err != nil {
				t.Fatal(err)
			}
			var encoded []byte
			switch fixture.Kind {
			case "ffi-request":
				var value ffiRequest
				value, err = decodeFFIRequest(wire, Limits{})
				if err == nil {
					if value.Operation != "close" || value.Lease != 7 {
						t.Fatalf("request: %+v", value)
					}
					encoded = encodeFFIRequest(value)
				}
			case "ffi-response":
				var value ffiResponse
				value, err = decodeFFIResponse(wire)
				if err == nil {
					if value.Lease != 7 {
						t.Fatalf("reply: %+v", value)
					}
					encoded = encodeFFIResponse(value)
				}
			case "endpoint":
				var value endpointFrame
				value, err = decodeEndpointFrame(wire, Limits{})
				if err == nil {
					if value.Origin != "p" || value.Kind != "ready" {
						t.Fatalf("frame: %+v", value)
					}
					encoded, err = encodeEndpointFrame(value, NormalizeLimits(Limits{}).MaxMessageBytes)
				}
			case "fragment":
				var value endpointFragment
				value, err = decodeEndpointFragment(wire, Limits{})
				if err == nil {
					if value.messageID != 1 || value.total != 3 || value.offset != 0 || string(value.data) != "abc" {
						t.Fatalf("fragment: %+v", value)
					}
					encoded, err = encodeEndpointFragment(value.messageID, int(value.total), int(value.offset), value.data, 256)
				}
			default:
				t.Fatalf("unknown fixture kind %q", fixture.Kind)
			}
			if fixture.Invalid {
				if err == nil {
					t.Fatal("invalid wire accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, wire) {
				t.Fatalf("wire: %x, want %x", encoded, wire)
			}
		})
	}
}

func TestSharedControlFrames(t *testing.T) {
	data, err := os.ReadFile("../testdata/rpc/wire/frames.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Kind, Hex, Code string
		ID, Target, Binding   uint64
		Reply                 bool
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			data, err := hex.DecodeString(fixture.Hex)
			if err != nil {
				t.Fatal(err)
			}
			frame, err := decodeEndpointFrame(data, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if frame.Kind != fixture.Kind || frame.ID != fixture.ID || frame.TargetID != fixture.Target || frame.Binding != fixture.Binding || frame.Reply != fixture.Reply || string(frame.Code) != fixture.Code {
				t.Fatalf("frame: %+v", frame)
			}
			encoded, err := encodeEndpointFrame(frame, NormalizeLimits(Limits{}).MaxMessageBytes)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, data) {
				t.Fatalf("canonical frame: %x", encoded)
			}
		})
	}
}

func TestSharedFragmentSequences(t *testing.T) {
	data, err := os.ReadFile("../testdata/rpc/wire/fragments.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		ID      string
		Frames  []string
		Results []*string
		ErrorAt int `json:"error_at"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.ID, func(t *testing.T) {
			var assembly endpointAssembly
			var previous uint64
			for index, encoded := range fixture.Frames {
				data, err := hex.DecodeString(encoded)
				if err != nil {
					t.Fatal(err)
				}
				fragment, err := decodeEndpointFragment(data, Limits{})
				var result []byte
				if err == nil {
					result, err = assembly.append(fragment, &previous)
				}
				if index+1 == fixture.ErrorAt {
					if err == nil {
						t.Fatal("invalid sequence accepted")
					}
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				expected := fixture.Results[index]
				if (result == nil) != (expected == nil) || expected != nil && string(result) != *expected {
					t.Fatalf("fragment %d: %q", index, result)
				}
			}
		})
	}
}
