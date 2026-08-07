package litz_test

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/cuprite-io/litz"
)

func TestSingleKey(t *testing.T) {
	input := &SingleKey{ID: 42}
	buf, err := MarshalSingleKey(input, nil)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var output SingleKey
	if err := UnmarshalSingleKey(buf, &output); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if output.ID != input.ID {
		t.Errorf("Mismatch: expected %d, got %d", input.ID, output.ID)
	}
}

func TestUserProfile(t *testing.T) {
	tests := []struct {
		name  string
		input *UserProfile
	}{
		{
			name: "Regular profile",
			input: &UserProfile{
				Age:    30,
				Score:  99.9,
				Name:   "Alice Smith",
				Active: true,
			},
		},
		{
			name: "Empty name",
			input: &UserProfile{
				Age:    18,
				Score:  0,
				Name:   "",
				Active: false,
			},
		},
		{
			name: "Very long name",
			input: &UserProfile{
				Age:    100,
				Score:  -12.34,
				Name:   "Some very very long name designed to test slice growth and alignment boundaries in the buffer allocator",
				Active: true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, err := MarshalUserProfile(tc.input, nil)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var output UserProfile
			if err := UnmarshalUserProfile(buf, &output); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if !reflect.DeepEqual(*tc.input, output) {
				t.Errorf("Mismatch:\nexpected: %+v\ngot: %+v", *tc.input, output)
			}
		})
	}
}

func TestNestedAndDynamicPayload(t *testing.T) {
	// 1. Prepare dynamic map data
	metadataMap := map[string]any{
		"env":      "production",
		"version":  12,
		"ratio":    1.618,
		"verified": true,
		"tags":     []any{"hft", "ultra-low-latency"},
	}

	// 2. Create nested payload struct
	input := &NestedPayload{
		Profile: &UserProfile{
			Age:    25,
			Score:  85.5,
			Name:   "Bob Vance",
			Active: true,
		},
		Metadata:  metadataMap,
		Tags:      []string{"tag1", "tag2", "super-long-tag-name-3"},
		Numbers:   []int{10, 20, 30, 42, 999999},
		Signature: []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0xFF},
	}

	buf, err := MarshalNestedPayload(input, nil)
	if err != nil {
		t.Fatalf("MarshalNestedPayload failed: %v", err)
	}

	var output NestedPayload
	if err := UnmarshalNestedPayload(buf, &output); err != nil {
		t.Fatalf("UnmarshalNestedPayload failed: %v", err)
	}

	// Verify primitive nested struct
	if !reflect.DeepEqual(*input.Profile, *output.Profile) {
		t.Errorf("Profile mismatch:\nexpected: %+v\ngot: %+v", *input.Profile, *output.Profile)
	}

	// Verify slices
	if !reflect.DeepEqual(input.Tags, output.Tags) {
		t.Errorf("Tags mismatch: expected %v, got %v", input.Tags, output.Tags)
	}
	if !reflect.DeepEqual(input.Numbers, output.Numbers) {
		t.Errorf("Numbers mismatch: expected %v, got %v", input.Numbers, output.Numbers)
	}
	if !bytes.Equal(input.Signature, output.Signature) {
		t.Errorf("Signature mismatch: expected %v, got %v", input.Signature, output.Signature)
	}

	// Verify Dynamic Metadata field (HIBI Engine)
	dyn, ok := output.Metadata.(*litz.Dynamic)
	if !ok || dyn == nil {
		t.Fatalf("Metadata was not unmarshaled as *Dynamic")
	}

	// Check fields using O(log N) binary search lookup
	if dyn.Get("env").String() != "production" {
		t.Errorf("Metadata.env: expected 'production', got %q", dyn.Get("env").String())
	}
	if dyn.Get("version").Int() != 12 {
		t.Errorf("Metadata.version: expected 12, got %d", dyn.Get("version").Int())
	}
	if dyn.Get("ratio").Float() != 1.618 {
		t.Errorf("Metadata.ratio: expected 1.618, got %f", dyn.Get("ratio").Float())
	}
	if !dyn.Get("verified").Bool() {
		t.Errorf("Metadata.verified: expected true, got false")
	}

	// Check dynamic nested slice
	tagsSlice := dyn.Get("tags").Slice()
	if len(tagsSlice) != 2 {
		t.Fatalf("Metadata.tags length mismatch: expected 2, got %d", len(tagsSlice))
	}
	if tagsSlice[0].String() != "hft" || tagsSlice[1].String() != "ultra-low-latency" {
		t.Errorf("Metadata.tags content mismatch")
	}
}

func TestOuterMessage(t *testing.T) {
	input := &OuterMessage{
		SeqNum: 99999999,
		Body: &NestedPayload{
			Profile: &UserProfile{
				Age:    45,
				Score:  12.3,
				Name:   "Nested inside outer",
				Active: false,
			},
			Tags:      []string{"nested-tag"},
			Numbers:   []int{7},
			Signature: []byte{0x01},
		},
	}

	buf, err := MarshalOuterMessage(input, nil)
	if err != nil {
		t.Fatalf("MarshalOuterMessage failed: %v", err)
	}

	var output OuterMessage
	if err := UnmarshalOuterMessage(buf, &output); err != nil {
		t.Fatalf("UnmarshalOuterMessage failed: %v", err)
	}

	if output.SeqNum != input.SeqNum {
		t.Errorf("SeqNum mismatch: expected %d, got %d", input.SeqNum, output.SeqNum)
	}

	if !reflect.DeepEqual(*input.Body.Profile, *output.Body.Profile) {
		t.Errorf("Nested Body.Profile mismatch")
	}

	if !reflect.DeepEqual(input.Body.Tags, output.Body.Tags) {
		t.Errorf("Nested Body.Tags mismatch")
	}
}

func TestNilNestedPointers(t *testing.T) {
	input := &OuterMessage{
		SeqNum: 101,
		Body:   nil, // Nil pointer test
	}

	buf, err := MarshalOuterMessage(input, nil)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var output OuterMessage
	if err := UnmarshalOuterMessage(buf, &output); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if output.SeqNum != input.SeqNum {
		t.Errorf("SeqNum mismatch: expected %d, got %d", input.SeqNum, output.SeqNum)
	}
	if output.Body != nil {
		t.Errorf("Expected nil Body, got non-nil struct")
	}
}

func TestHashCollision(t *testing.T) {
	// 1. Find two strings that collide under FNV-1a 32-bit
	seen := make(map[uint32]string)
	var key1, key2 string
	for i := 0; ; i++ {
		s := fmt.Sprintf("k_%d", i)
		h := litz.HashKey(s)
		if prev, ok := seen[h]; ok {
			key1 = prev
			key2 = s
			break
		}
		seen[h] = s
	}

	t.Logf("Found FNV-1a 32-bit collision: %q and %q both hash to %d", key1, key2, litz.HashKey(key1))

	// 2. Put both colliding keys into a dynamic map with different values
	m := map[string]any{
		key1: "value_one",
		key2: "value_two",
	}

	buf, _, err := litz.MarshalAny(m)
	if err != nil {
		t.Fatalf("MarshalAny failed: %v", err)
	}

	dyn := litz.NewDynamic(buf, litz.TypeMap)

	// 3. Verify that we can retrieve both values correctly (proving no silent collision corruption)
	val1 := dyn.Get(key1)
	if val1 == nil || val1.String() != "value_one" {
		t.Errorf("Expected key1 to return 'value_one', got %q", val1.String())
	}

	val2 := dyn.Get(key2)
	if val2 == nil || val2.String() != "value_two" {
		t.Errorf("Expected key2 to return 'value_two', got %q", val2.String())
	}
}

func TestDynamicKeysLenAndSliceType(t *testing.T) {
	// Test Keys() and Len() on a map
	m := map[string]any{
		"a": int(1),
		"b": "hello",
		"c": true,
	}
	buf, _, err := litz.MarshalAny(m)
	if err != nil {
		t.Fatalf("MarshalAny failed: %v", err)
	}

	dyn := litz.NewDynamic(buf, litz.TypeMap)
	if dyn.Len() != 3 {
		t.Errorf("Expected Len() to be 3, got %d", dyn.Len())
	}

	keys := dyn.Keys()
	sort.Strings(keys)
	expectedKeys := []string{"a", "b", "c"}
	if !reflect.DeepEqual(keys, expectedKeys) {
		t.Errorf("Expected keys %v, got %v", expectedKeys, keys)
	}

	// Test Slice element type propagation
	s := []any{"one", "two", "three"}
	sliceBuf, _, err := litz.MarshalAny(s)
	if err != nil {
		t.Fatalf("MarshalAny slice failed: %v", err)
	}

	dynSlice := litz.NewDynamic(sliceBuf, litz.TypeSlice)
	if dynSlice.Len() != 3 {
		t.Errorf("Expected slice Len() to be 3, got %d", dynSlice.Len())
	}

	elements := dynSlice.Slice()
	if len(elements) != 3 {
		t.Fatalf("Expected 3 slice elements, got %d", len(elements))
	}

	for i, el := range elements {
		if el.Type() != litz.TypeString {
			t.Errorf("Expected slice element type to be TypeString (%d), got %d at index %d", litz.TypeString, el.Type(), i)
		}
	}

	// Test CloneAny deep copy
	originalMap := map[string]any{
		"sub_map": map[string]any{
			"key": "val",
		},
		"slice": []any{"x", "y"},
	}
	cloned := litz.CloneAny(originalMap)
	if !reflect.DeepEqual(originalMap, cloned) {
		t.Errorf("Expected cloned to match original, got %v", cloned)
	}
}

func TestGroupAFixes(t *testing.T) {
	// 1. Map key > 255 bytes validation
	longKey := make([]byte, 256)
	for i := range longKey {
		longKey[i] = 'a'
	}
	m := map[string]any{string(longKey): 42}
	_, _, err := litz.MarshalAny(m)
	if err == nil {
		t.Error("Expected error when marshaling map with key > 255 bytes, got nil")
	}

	// 2. Heterogeneous slice validation
	slice := []any{"hello", int(42)}
	_, _, err = litz.MarshalAny(slice)
	if err == nil {
		t.Error("Expected error when marshaling heterogeneous slice, got nil")
	}

	// 3. uint64 dynamic type round-trip
	val := uint64(18446744073709551615)
	buf, valType, err := litz.MarshalAny(val)
	if err != nil {
		t.Fatalf("Marshal uint64 failed: %v", err)
	}
	if valType != 8 { // TypeUint
		t.Errorf("Expected TypeUint (8), got %d", valType)
	}
	dyn := litz.NewDynamic(buf, valType)
	if dyn.Uint() != val {
		t.Errorf("Expected uint64 %d, got %d", val, dyn.Uint())
	}

	// 4. Type validation in accessors
	strBuf, _, _ := litz.MarshalAny("hello")
	dynStr := litz.NewDynamic(strBuf, litz.TypeString)
	if dynStr.Int() != 0 {
		t.Errorf("Expected Int() to validate type and return 0, got %d", dynStr.Int())
	}
	if dynStr.Float() != 0.0 {
		t.Errorf("Expected Float() to validate type and return 0.0, got %f", dynStr.Float())
	}
	if dynStr.Bool() != false {
		t.Error("Expected Bool() to validate type and return false")
	}
}

