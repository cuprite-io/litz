package litz_test

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unsafe"

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

func TestDynamicGetView(t *testing.T) {
	buf, _, err := litz.MarshalAny(map[string]any{
		"env":   "production",
		"count": int64(42),
	})
	if err != nil {
		t.Fatalf("MarshalAny failed: %v", err)
	}
	dyn := litz.NewDynamic(buf, litz.TypeMap)

	env, ok := dyn.GetView("env")
	if !ok || env.Type() != litz.TypeString || env.String() != "production" {
		t.Fatalf("GetView(env) = (%v, %t), want (production, true)", env.String(), ok)
	}

	count, ok := dyn.GetView("count")
	if !ok || count.Type() != litz.TypeInt || count.Int() != 42 {
		t.Fatalf("GetView(count) = (%d, %t), want (42, true)", count.Int(), ok)
	}

	if _, ok := dyn.GetView("missing"); ok {
		t.Fatal("GetView returned a value for a missing key")
	}

	// Get and GetOK must remain behaviorally identical to GetView.
	if got := dyn.Get("env"); got == nil || got.String() != "production" {
		t.Fatalf("Get(env) = %v, want production", got)
	}
	if got, ok := dyn.GetOK("count"); !ok || got.Int() != 42 {
		t.Fatalf("GetOK(count) = (%v, %t), want (42, true)", got, ok)
	}
}

func TestDynamicGetViewAllocations(t *testing.T) {
	buf, _, err := litz.MarshalAny(map[string]any{"env": "production"})
	if err != nil {
		t.Fatalf("MarshalAny failed: %v", err)
	}
	dyn := litz.NewDynamic(buf, litz.TypeMap)

	var got string
	allocs := testing.AllocsPerRun(1000, func() {
		val, ok := dyn.GetView("env")
		if ok {
			got = val.String()
		} else {
			got = ""
		}
	})
	if got != "production" {
		t.Fatalf("GetView returned %q, want production", got)
	}
	if allocs != 0 {
		t.Fatalf("GetView allocated %.1f times per lookup, want 0", allocs)
	}
}

func TestHIBIEngine_SafetyAndCorrectness(t *testing.T) {
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

func TestGenerator_BoundsSafetyAndOverflows(t *testing.T) {
	// Test Issue 12: Unmarshal bounds check bypass mitigation
	profile := &UserProfile{
		Age:    25,
		Score:  9.5,
		Name:   "Test User",
		Active: true,
	}
	buf, err := MarshalUserProfile(profile, nil)
	if err != nil {
		t.Fatalf("MarshalUserProfile failed: %v", err)
	}

	// 1. Verify format signature header check
	corruptHeader := make([]byte, len(buf))
	copy(corruptHeader, buf)
	corruptHeader[0] = 'X'
	var output UserProfile
	err = UnmarshalUserProfile(corruptHeader, &output)
	if err != litz.ErrInvalidHeader {
		t.Errorf("Expected ErrInvalidHeader, got %v", err)
	}

	// 2. Corrupt Name.offset field (at offset 24: 8-byte version header + 16-byte mirror offset)
	// Write MaxUint64 (0xFFFFFFFFFFFFFFFF)
	*(*uint64)(unsafe.Pointer(&buf[24])) = ^uint64(0)

	err = UnmarshalUserProfile(buf, &output)
	if err == nil {
		t.Error("Expected ErrStringOutOfBounds on corrupted offset, got nil")
	}
}

func TestGroupCFixes(t *testing.T) {
	// 1. time.Time round-trip in MarshalAny / Dynamic
	now := time.Now().Round(time.Microsecond) // FNV nanosecond resolution
	buf, valType, err := litz.MarshalAny(now)
	if err != nil {
		t.Fatalf("MarshalAny(time.Time) failed: %v", err)
	}
	if valType != litz.TypeInt {
		t.Errorf("Expected TypeInt for time.Time, got %d", valType)
	}
	dyn := litz.NewDynamic(buf, valType)
	parsedTime := time.Unix(0, dyn.Int())
	if !parsedTime.Equal(now) {
		t.Errorf("Expected time %v, got %v", now, parsedTime)
	}

	// 2. [16]byte UUID round-trip in MarshalAny / Dynamic
	uuidVal := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	bufUUID, valTypeUUID, err := litz.MarshalAny(uuidVal)
	if err != nil {
		t.Fatalf("MarshalAny([16]byte) failed: %v", err)
	}
	if valTypeUUID != litz.TypeBytes {
		t.Errorf("Expected TypeBytes for [16]byte, got %d", valTypeUUID)
	}
	dynUUID := litz.NewDynamic(bufUUID, valTypeUUID)
	var outUUID [16]byte
	copy(outUUID[:], dynUUID.Bytes())
	if outUUID != uuidVal {
		t.Errorf("Expected UUID %v, got %v", uuidVal, outUUID)
	}

	// 3. Reject non-string keys in maps
	invalidMap := map[int]string{42: "hello"}
	_, _, err = litz.MarshalAny(invalidMap)
	if err == nil {
		t.Error("Expected error when marshaling map with non-string keys, got nil")
	}

	// 4. Dynamic.ToMap and ToSlice round-trip
	m := map[string]any{
		"name": "John",
		"age":  int64(30),
		"tags": []any{"developer", "gopher"},
	}
	mapBuf, mapType, err := litz.MarshalAny(m)
	if err != nil {
		t.Fatalf("MarshalAny(map) failed: %v", err)
	}
	dynMap := litz.NewDynamic(mapBuf, mapType)
	resMap, err := dynMap.ToMap()
	if err != nil {
		t.Fatalf("ToMap failed: %v", err)
	}
	if resMap["name"] != "John" {
		t.Errorf("Expected name to be John, got %v", resMap["name"])
	}
	if resMap["age"] != int64(30) {
		t.Errorf("Expected age to be 30, got %v", resMap["age"])
	}
	tagsSlice, ok := resMap["tags"].([]any)
	if !ok {
		t.Fatalf("Expected tags to be []any, got %T", resMap["tags"])
	}
	if tagsSlice[0] != "developer" || tagsSlice[1] != "gopher" {
		t.Errorf("Unexpected tags contents: %v", tagsSlice)
	}
}

func TestSchemaEvolution_BackwardCompatibility(t *testing.T) {
	// Simulate an older serialized payload of UserProfile that only contains Age and Score.
	// 8-byte version header + 4-byte Age + 4-byte padding + 8-byte Score = 24 bytes.
	buf := litz.EnsureAligned(make([]byte, 24))

	// Format signature and version
	*(*[4]byte)(unsafe.Pointer(&buf[0])) = [4]byte{'L', 'T', 'Z', '0'}
	buf[4] = 1 // Version 1

	// Set Age = 42 (Age starts at mirror offset 8)
	*(*uint32)(unsafe.Pointer(&buf[8])) = 42

	// Set Score = 3.14 (Score starts at mirror offset 16)
	*(*float64)(unsafe.Pointer(&buf[16])) = 3.14

	var output UserProfile
	err := UnmarshalUserProfile(buf, &output)
	if err != nil {
		t.Fatalf("Expected UnmarshalUserProfile to succeed on shorter old payload, got: %v", err)
	}

	if output.Age != 42 {
		t.Errorf("Expected Age = 42, got %d", output.Age)
	}
	if output.Score != 3.14 {
		t.Errorf("Expected Score = 3.14, got %f", output.Score)
	}
	if output.Name != "" {
		t.Errorf("Expected Name to be empty default, got %q", output.Name)
	}
	if output.Active != false {
		t.Errorf("Expected Active to be false default, got %t", output.Active)
	}
}

func FuzzUnmarshalUserProfile(f *testing.F) {
	// Seed corpus with a valid payload
	profile := &UserProfile{
		Age:    25,
		Score:  9.5,
		Name:   "Test User",
		Active: true,
	}
	buf, err := MarshalUserProfile(profile, nil)
	if err == nil {
		f.Add(buf)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var output UserProfile
		_ = UnmarshalUserProfile(data, &output)
	})
}

func TestGeneratedUnmarshalAcceptsEmptyMirror(t *testing.T) {
	header := func() []byte {
		return []byte{'L', 'T', 'Z', '0', 1, 0, 0, 0}
	}

	t.Run("SingleKey", func(t *testing.T) {
		var out SingleKey
		if err := UnmarshalSingleKey(header(), &out); err != nil {
			t.Fatalf("UnmarshalSingleKey failed: %v", err)
		}
		if out.ID != 0 {
			t.Fatalf("ID = %d, want 0", out.ID)
		}
	})

	t.Run("UserProfile", func(t *testing.T) {
		var out UserProfile
		if err := UnmarshalUserProfile(header(), &out); err != nil {
			t.Fatalf("UnmarshalUserProfile failed: %v", err)
		}
		if out != (UserProfile{}) {
			t.Fatalf("output = %+v, want zero value", out)
		}
	})

	t.Run("NestedPayload", func(t *testing.T) {
		var out NestedPayload
		if err := UnmarshalNestedPayload(header(), &out); err != nil {
			t.Fatalf("UnmarshalNestedPayload failed: %v", err)
		}
		if out.Profile != nil || out.Metadata != nil || out.Tags != nil || out.Numbers != nil || out.Signature != nil {
			t.Fatalf("output = %+v, want zero value", out)
		}
	})

	t.Run("OuterMessage", func(t *testing.T) {
		var out OuterMessage
		if err := UnmarshalOuterMessage(header(), &out); err != nil {
			t.Fatalf("UnmarshalOuterMessage failed: %v", err)
		}
		if out.SeqNum != 0 || out.Body != nil {
			t.Fatalf("output = %+v, want zero value", out)
		}
	})

	t.Run("RecursiveNode", func(t *testing.T) {
		var out RecursiveNode
		if err := UnmarshalRecursiveNode(header(), &out); err != nil {
			t.Fatalf("UnmarshalRecursiveNode failed: %v", err)
		}
		if out.Val != 0 || out.Next != nil {
			t.Fatalf("output = %+v, want zero value", out)
		}
	})
}

func TestUnmarshalNestedPayloadRejectsMalformedStringHeaders(t *testing.T) {
	input := &NestedPayload{
		Tags:      []string{"alpha", "beta", "gamma"},
		Numbers:   []int{1, 2, 3},
		Signature: []byte("sig"),
	}
	buf, err := MarshalNestedPayload(input, nil)
	if err != nil {
		t.Fatalf("MarshalNestedPayload failed: %v", err)
	}

	stringHeaders := func(b []byte) []struct {
		offset uintptr
		length int
	} {
		mirror := (*nestedPayloadLitzMirror)(unsafe.Pointer(&b[8]))
		return unsafe.Slice((*struct {
			offset uintptr
			length int
		})(unsafe.Add(unsafe.Pointer(&b[0]), mirror.Tags.offset)), mirror.Tags.length)
	}

	checkRejected := func(t *testing.T, mutate func([]byte)) {
		t.Helper()
		clone := append([]byte(nil), buf...)
		mutate(clone)

		var output NestedPayload
		if err := UnmarshalNestedPayload(clone, &output); err != litz.ErrStringOutOfBounds {
			t.Fatalf("UnmarshalNestedPayload error = %v, want %v", err, litz.ErrStringOutOfBounds)
		}
	}

	t.Run("offset overflows int", func(t *testing.T) {
		checkRejected(t, func(b []byte) {
			stringHeaders(b)[0].offset = ^uintptr(0)
		})
	})

	t.Run("negative length", func(t *testing.T) {
		checkRejected(t, func(b []byte) {
			stringHeaders(b)[0].length = -1
		})
	})

	t.Run("length exceeds remaining buffer", func(t *testing.T) {
		checkRejected(t, func(b []byte) {
			h := &stringHeaders(b)[0]
			h.offset = uintptr(len(b)) - 1
			h.length = 2
		})
	})

	t.Run("nonzero length at end of buffer", func(t *testing.T) {
		checkRejected(t, func(b []byte) {
			h := &stringHeaders(b)[0]
			h.offset = uintptr(len(b))
			h.length = 1
		})
	})
}

func TestMarshalZeroLengthCapacityBuffer(t *testing.T) {
	t.Run("SingleKey", func(t *testing.T) {
		input := &SingleKey{ID: 42}
		buf := make([]byte, 0, 1024)
		out, err := MarshalSingleKey(input, buf)
		if err != nil {
			t.Fatalf("MarshalSingleKey failed: %v", err)
		}
		if &out[0] != &buf[:1024][0] {
			t.Fatalf("expected Marshal to reuse backing array of aligned slice")
		}
		var decoded SingleKey
		if err := UnmarshalSingleKey(out, &decoded); err != nil {
			t.Fatalf("UnmarshalSingleKey failed: %v", err)
		}
		if decoded.ID != 42 {
			t.Fatalf("decoded.ID = %d, want 42", decoded.ID)
		}
	})

	t.Run("UserProfile", func(t *testing.T) {
		input := &UserProfile{Age: 30, Score: 98.5, Name: "Alice", Active: true}
		buf := make([]byte, 0, 1024)
		out, err := MarshalUserProfile(input, buf)
		if err != nil {
			t.Fatalf("MarshalUserProfile failed: %v", err)
		}
		if &out[0] != &buf[:1024][0] {
			t.Fatalf("expected Marshal to reuse backing array of aligned slice")
		}
		var decoded UserProfile
		if err := UnmarshalUserProfile(out, &decoded); err != nil {
			t.Fatalf("UnmarshalUserProfile failed: %v", err)
		}
		if decoded.Age != 30 || decoded.Score != 98.5 || decoded.Name != "Alice" || !decoded.Active {
			t.Fatalf("decoded UserProfile mismatch: %+v", decoded)
		}
	})

	t.Run("NestedPayload", func(t *testing.T) {
		input := &NestedPayload{
			Profile:   &UserProfile{Age: 25, Score: 88.0, Name: "Bob", Active: false},
			Tags:      []string{"tag1", "tag2"},
			Numbers:   []int{10, 20, 30},
			Signature: []byte("secure-sig"),
		}
		buf := make([]byte, 0, 2048)
		out, err := MarshalNestedPayload(input, buf)
		if err != nil {
			t.Fatalf("MarshalNestedPayload failed: %v", err)
		}
		if &out[0] != &buf[:2048][0] {
			t.Fatalf("expected Marshal to reuse backing array of aligned slice")
		}
		var decoded NestedPayload
		if err := UnmarshalNestedPayload(out, &decoded); err != nil {
			t.Fatalf("UnmarshalNestedPayload failed: %v", err)
		}
		if decoded.Profile == nil || decoded.Profile.Age != 25 || decoded.Profile.Name != "Bob" {
			t.Fatalf("decoded profile mismatch: %+v", decoded.Profile)
		}
		if len(decoded.Tags) != 2 || decoded.Tags[0] != "tag1" || decoded.Tags[1] != "tag2" {
			t.Fatalf("decoded tags mismatch: %v", decoded.Tags)
		}
		if len(decoded.Numbers) != 3 || decoded.Numbers[0] != 10 {
			t.Fatalf("decoded numbers mismatch: %v", decoded.Numbers)
		}
		if string(decoded.Signature) != "secure-sig" {
			t.Fatalf("decoded signature mismatch: %s", decoded.Signature)
		}
	})

	t.Run("OuterMessage", func(t *testing.T) {
		input := &OuterMessage{
			SeqNum: 777,
			Body: &NestedPayload{
				Tags: []string{"msg"},
			},
		}
		buf := make([]byte, 0, 2048)
		out, err := MarshalOuterMessage(input, buf)
		if err != nil {
			t.Fatalf("MarshalOuterMessage failed: %v", err)
		}
		if &out[0] != &buf[:2048][0] {
			t.Fatalf("expected Marshal to reuse backing array of aligned slice")
		}
		var decoded OuterMessage
		if err := UnmarshalOuterMessage(out, &decoded); err != nil {
			t.Fatalf("UnmarshalOuterMessage failed: %v", err)
		}
		if decoded.SeqNum != 777 || decoded.Body == nil || len(decoded.Body.Tags) != 1 || decoded.Body.Tags[0] != "msg" {
			t.Fatalf("decoded OuterMessage mismatch: %+v", decoded)
		}
	})

	t.Run("RecursiveNode", func(t *testing.T) {
		input := &RecursiveNode{Val: 99}
		buf := make([]byte, 0, 1024)
		out, err := MarshalRecursiveNode(input, buf)
		if err != nil {
			t.Fatalf("MarshalRecursiveNode failed: %v", err)
		}
		if &out[0] != &buf[:1024][0] {
			t.Fatalf("expected Marshal to reuse backing array of aligned slice")
		}
		var decoded RecursiveNode
		if err := UnmarshalRecursiveNode(out, &decoded); err != nil {
			t.Fatalf("UnmarshalRecursiveNode failed: %v", err)
		}
		if decoded.Val != 99 || decoded.Next != nil {
			t.Fatalf("decoded RecursiveNode mismatch: %+v", decoded)
		}
	})
}

func TestRecursionLimits_MarshalAny(t *testing.T) {
	t.Run("CyclicMap", func(t *testing.T) {
		m := make(map[string]any)
		m["self"] = m
		_, _, err := litz.MarshalAny(m)
		if err != litz.ErrMaxDepthExceeded {
			t.Fatalf("MarshalAny on cyclic map error = %v, want %v", err, litz.ErrMaxDepthExceeded)
		}
	})

	t.Run("CyclicSlice", func(t *testing.T) {
		s := make([]any, 1)
		s[0] = s
		_, _, err := litz.MarshalAny(s)
		if err != litz.ErrMaxDepthExceeded {
			t.Fatalf("MarshalAny on cyclic slice error = %v, want %v", err, litz.ErrMaxDepthExceeded)
		}
	})

	t.Run("DeeplyNestedMap", func(t *testing.T) {
		root := make(map[string]any)
		curr := root
		for i := 0; i < litz.DefaultMaxDepth+5; i++ {
			next := make(map[string]any)
			curr["child"] = next
			curr = next
		}
		_, _, err := litz.MarshalAny(root)
		if err != litz.ErrMaxDepthExceeded {
			t.Fatalf("MarshalAny on deeply nested map error = %v, want %v", err, litz.ErrMaxDepthExceeded)
		}
	})

	t.Run("ValidNestedMap", func(t *testing.T) {
		root := make(map[string]any)
		curr := root
		for i := 0; i < 5; i++ {
			next := make(map[string]any)
			curr["child"] = next
			curr = next
		}
		curr["val"] = "leaf"
		buf, valType, err := litz.MarshalAny(root)
		if err != nil {
			t.Fatalf("MarshalAny failed on valid nested map: %v", err)
		}
		dyn := litz.NewDynamic(buf, valType)
		res := dyn.Interface()
		if res == nil {
			t.Fatalf("dyn.Interface returned nil for valid map")
		}
	})
}

func TestRecursionLimits_CloneAny(t *testing.T) {
	t.Run("CyclicMap", func(t *testing.T) {
		m := make(map[string]any)
		m["self"] = m
		// Should terminate cleanly and not panic with stack overflow
		cloned := litz.CloneAny(m)
		if cloned == nil {
			t.Fatalf("expected non-nil top-level cloned map")
		}
	})

	t.Run("CyclicSlice", func(t *testing.T) {
		s := make([]any, 1)
		s[0] = s
		// Should terminate cleanly and not panic with stack overflow
		cloned := litz.CloneAny(s)
		if cloned == nil {
			t.Fatalf("expected non-nil top-level cloned slice")
		}
	})
}

func TestRecursionLimits_DynamicInterface(t *testing.T) {
	t.Run("DeeplyNestedDynamicMap", func(t *testing.T) {
		root := make(map[string]any)
		curr := root
		for i := 0; i < 10; i++ {
			next := make(map[string]any)
			curr["child"] = next
			curr = next
		}
		curr["val"] = int64(123)

		buf, valType, err := litz.MarshalAny(root)
		if err != nil {
			t.Fatalf("MarshalAny failed: %v", err)
		}
		dyn := litz.NewDynamic(buf, valType)
		val := dyn.Interface()
		if val == nil {
			t.Fatalf("dyn.Interface failed on nested dynamic map")
		}
	})
}

func TestRecursionLimits_RecursiveNode(t *testing.T) {
	t.Run("CyclicMarshal", func(t *testing.T) {
		n := &RecursiveNode{Val: 1}
		n.Next = n // cycle!
		_, err := MarshalRecursiveNode(n, nil)
		if err != litz.ErrMaxDepthExceeded {
			t.Fatalf("MarshalRecursiveNode on cyclic struct error = %v, want %v", err, litz.ErrMaxDepthExceeded)
		}
	})

	t.Run("CyclicClone", func(t *testing.T) {
		n := &RecursiveNode{Val: 1}
		n.Next = n // cycle!
		// Clone should terminate cleanly without stack overflow
		cloned := n.Clone()
		if cloned == nil {
			t.Fatalf("expected non-nil top-level cloned struct")
		}
	})

	t.Run("DeeplyNestedNodes", func(t *testing.T) {
		root := &RecursiveNode{Val: 0}
		curr := root
		for i := 1; i <= litz.DefaultMaxDepth+5; i++ {
			curr.Next = &RecursiveNode{Val: int64(i)}
			curr = curr.Next
		}
		_, err := MarshalRecursiveNode(root, nil)
		if err != litz.ErrMaxDepthExceeded {
			t.Fatalf("MarshalRecursiveNode on deeply nested nodes error = %v, want %v", err, litz.ErrMaxDepthExceeded)
		}
	})

	t.Run("ValidNestedNodes", func(t *testing.T) {
		root := &RecursiveNode{Val: 0}
		curr := root
		for i := 1; i <= 5; i++ {
			curr.Next = &RecursiveNode{Val: int64(i)}
			curr = curr.Next
		}
		buf, err := MarshalRecursiveNode(root, nil)
		if err != nil {
			t.Fatalf("MarshalRecursiveNode failed: %v", err)
		}
		var decoded RecursiveNode
		if err := UnmarshalRecursiveNode(buf, &decoded); err != nil {
			t.Fatalf("UnmarshalRecursiveNode failed: %v", err)
		}
		if decoded.Val != 0 || decoded.Next == nil || decoded.Next.Val != 1 {
			t.Fatalf("decoded RecursiveNode mismatch: %+v", decoded)
		}
	})

	t.Run("MaxDepthExceededInUnmarshal", func(t *testing.T) {
		node := &RecursiveNode{Val: 1}
		buf, err := MarshalRecursiveNode(node, nil)
		if err != nil {
			t.Fatalf("MarshalRecursiveNode failed: %v", err)
		}
		var out RecursiveNode
		err = unmarshalRecursiveNodeWithDepth(buf, &out, litz.DefaultMaxDepth+1)
		if err != litz.ErrMaxDepthExceeded {
			t.Fatalf("expected ErrMaxDepthExceeded, got %v", err)
		}
	})
}

func TestMarshalUnalignedBufferFallback(t *testing.T) {
	input := &UserProfile{Age: 99, Score: 77.7, Name: "UnalignedTest", Active: true}
	// Create an unaligned slice with sufficient capacity by offsetting an aligned buffer by 1 byte
	aligned := litz.AlignedBuffer(1024)
	unaligned := aligned[1:1:1024] // len=0, cap=1023, address % 8 != 0

	out, err := MarshalUserProfile(input, unaligned)
	if err != nil {
		t.Fatalf("MarshalUserProfile with unaligned buffer failed: %v", err)
	}
	// Must have allocated a fresh aligned buffer instead of using unaligned slice
	if uintptr(unsafe.Pointer(&out[0]))%8 != 0 {
		t.Fatalf("MarshalUserProfile returned unaligned output buffer")
	}
	var decoded UserProfile
	if err := UnmarshalUserProfile(out, &decoded); err != nil {
		t.Fatalf("UnmarshalUserProfile failed: %v", err)
	}
	if decoded.Age != 99 || decoded.Score != 77.7 || decoded.Name != "UnalignedTest" || !decoded.Active {
		t.Fatalf("decoded mismatch: %+v", decoded)
	}
}

func FuzzUnmarshalNestedPayload(f *testing.F) {
	input := &NestedPayload{
		Tags:      []string{"alpha", "beta", "gamma"},
		Numbers:   []int{1, 2, 3},
		Signature: []byte("sig"),
	}
	buf, err := MarshalNestedPayload(input, nil)
	if err == nil {
		f.Add(buf)
	}
	f.Add([]byte{'L', 'T', 'Z', '0', 1, 0, 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		var output NestedPayload
		_ = UnmarshalNestedPayload(data, &output)
	})
}

func FuzzUnmarshalSingleKey(f *testing.F) {
	single := &SingleKey{ID: 100}
	buf, err := MarshalSingleKey(single, nil)
	if err == nil {
		f.Add(buf)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var output SingleKey
		_ = UnmarshalSingleKey(data, &output)
	})
}

func FuzzUnmarshalOuterMessage(f *testing.F) {
	input := &OuterMessage{
		SeqNum: 42,
		Body: &NestedPayload{
			Profile: &UserProfile{Age: 25, Name: "FuzzUser"},
			Tags:    []string{"tag1", "tag2"},
		},
	}
	buf, err := MarshalOuterMessage(input, nil)
	if err == nil {
		f.Add(buf)
	}
	f.Add([]byte{'L', 'T', 'Z', '0', 1, 0, 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		var output OuterMessage
		_ = UnmarshalOuterMessage(data, &output)
		_ = UnmarshalOuterMessageStrict(data, &output)
	})
}

func FuzzUnmarshalRecursiveNode(f *testing.F) {
	input := &RecursiveNode{
		Val: 1,
		Next: &RecursiveNode{
			Val: 2,
			Next: &RecursiveNode{
				Val: 3,
			},
		},
	}
	buf, err := MarshalRecursiveNode(input, nil)
	if err == nil {
		f.Add(buf)
	}
	f.Add([]byte{'L', 'T', 'Z', '0', 1, 0, 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		var output RecursiveNode
		_ = UnmarshalRecursiveNode(data, &output)
		_ = UnmarshalRecursiveNodeStrict(data, &output)
	})
}

func FuzzDynamic(f *testing.F) {
	mapBuf, mapType, err := litz.MarshalAny(map[string]any{"key1": "val1", "num": int64(42)})
	if err == nil {
		f.Add(mapBuf, uint8(mapType))
	}
	sliceBuf, sliceType, err := litz.MarshalAny([]any{"item1", "item2"})
	if err == nil {
		f.Add(sliceBuf, uint8(sliceType))
	}
	f.Add([]byte{}, uint8(litz.TypeNull))
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8}, uint8(litz.TypeMap))
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8}, uint8(litz.TypeSlice))

	f.Fuzz(func(t *testing.T, data []byte, valType uint8) {
		dyn := litz.NewDynamic(data, valType)
		_ = dyn.Len()
		_ = dyn.Keys()
		_ = dyn.Get("key1")
		_, _ = dyn.GetView("key1")
		_, _ = dyn.GetOK("key1")
		_ = dyn.Slice()
		_ = dyn.Int()
		_ = dyn.Uint()
		_ = dyn.Float()
		_ = dyn.Bool()
		_ = dyn.String()
		_ = dyn.Bytes()
		_, _ = dyn.ToMap()
		_, _ = dyn.ToSlice()
		_ = litz.CloneAny(dyn)
	})
}

func TestUnmarshalRejectsUnalignedBuffer(t *testing.T) {
	t.Run("UserProfile", func(t *testing.T) {
		input := &UserProfile{Age: 30, Score: 99.5, Name: "Alice", Active: true}
		buf, err := MarshalUserProfile(input, nil)
		if err != nil {
			t.Fatalf("MarshalUserProfile failed: %v", err)
		}
		// Create an unaligned buffer by taking a subslice from offset 1
		unaligned := make([]byte, len(buf)+1)
		copy(unaligned[1:], buf)
		var out UserProfile
		err = UnmarshalUserProfile(unaligned[1:], &out)
		if err != litz.ErrUnalignedBuffer {
			t.Fatalf("expected ErrUnalignedBuffer, got: %v", err)
		}
	})

	t.Run("NestedPayload", func(t *testing.T) {
		input := &NestedPayload{
			Profile:   &UserProfile{Age: 20, Name: "Bob"},
			Tags:      []string{"go", "litz"},
			Numbers:   []int{1, 2},
			Signature: []byte("sign"),
		}
		buf, err := MarshalNestedPayload(input, nil)
		if err != nil {
			t.Fatalf("MarshalNestedPayload failed: %v", err)
		}
		unaligned := make([]byte, len(buf)+1)
		copy(unaligned[1:], buf)
		var out NestedPayload
		err = UnmarshalNestedPayload(unaligned[1:], &out)
		if err != litz.ErrUnalignedBuffer {
			t.Fatalf("expected ErrUnalignedBuffer, got: %v", err)
		}
	})

	t.Run("SingleKey", func(t *testing.T) {
		input := &SingleKey{ID: 12345}
		buf, err := MarshalSingleKey(input, nil)
		if err != nil {
			t.Fatalf("MarshalSingleKey failed: %v", err)
		}
		unaligned := make([]byte, len(buf)+1)
		copy(unaligned[1:], buf)
		var out SingleKey
		err = UnmarshalSingleKey(unaligned[1:], &out)
		if err != litz.ErrUnalignedBuffer {
			t.Fatalf("expected ErrUnalignedBuffer, got: %v", err)
		}
	})
}

func TestVariableSegment8ByteAlignment(t *testing.T) {
	input := &NestedPayload{
		Profile: &UserProfile{
			Age:    42,
			Score:  3.14,
			Name:   "OddLen", // len 6
			Active: true,
		},
		Metadata:  map[string]any{"env": "test"},
		Tags:      []string{"a", "bb", "ccc"}, // odd length strings
		Numbers:   []int{10, 20, 30},
		Signature: []byte("12345"), // len 5 (unaligned)
	}

	buf, err := MarshalNestedPayload(input, nil)
	if err != nil {
		t.Fatalf("MarshalNestedPayload failed: %v", err)
	}

	// 1. Total size must be a multiple of 8
	if len(buf)%8 != 0 {
		t.Fatalf("Total buffer size %d is not a multiple of 8", len(buf))
	}

	// 2. Base buffer must be 8-byte aligned
	if uintptr(unsafe.Pointer(&buf[0]))%8 != 0 {
		t.Fatalf("Buffer address is not 8-byte aligned: %p", &buf[0])
	}

	// 3. Inspect mirror variable offsets directly
	mirror := (*nestedPayloadLitzMirror)(unsafe.Pointer(&buf[8]))

	if mirror.Profile > 0 && mirror.Profile%8 != 0 {
		t.Errorf("Profile offset %d is not 8-byte aligned", mirror.Profile)
	}
	if mirror.Metadata.length > 0 && mirror.Metadata.offset%8 != 0 {
		t.Errorf("Metadata offset %d is not 8-byte aligned", mirror.Metadata.offset)
	}
	if mirror.Tags.length > 0 && mirror.Tags.offset%8 != 0 {
		t.Errorf("Tags offset %d is not 8-byte aligned", mirror.Tags.offset)
	}
	if mirror.Numbers.length > 0 && mirror.Numbers.offset%8 != 0 {
		t.Errorf("Numbers offset %d is not 8-byte aligned", mirror.Numbers.offset)
	}
	if mirror.Signature.length > 0 && mirror.Signature.offset%8 != 0 {
		t.Errorf("Signature offset %d is not 8-byte aligned", mirror.Signature.offset)
	}

	// 4. Verify unmarshaling succeeds and matches original input
	var decoded NestedPayload
	if err := UnmarshalNestedPayload(buf, &decoded); err != nil {
		t.Fatalf("UnmarshalNestedPayload failed: %v", err)
	}
	if decoded.Profile.Name != "OddLen" || decoded.Profile.Age != 42 {
		t.Errorf("Profile mismatch: %+v", decoded.Profile)
	}
	if len(decoded.Tags) != 3 || decoded.Tags[0] != "a" || decoded.Tags[1] != "bb" || decoded.Tags[2] != "ccc" {
		t.Errorf("Tags mismatch: %v", decoded.Tags)
	}
	if len(decoded.Numbers) != 3 || decoded.Numbers[0] != 10 {
		t.Errorf("Numbers mismatch: %v", decoded.Numbers)
	}
	if string(decoded.Signature) != "12345" {
		t.Errorf("Signature mismatch: %s", decoded.Signature)
	}
}

func TestUnmarshalVariableSegmentRejectsUnalignedOffset(t *testing.T) {
	input := &NestedPayload{
		Profile: &UserProfile{Age: 25, Name: "Tester"},
		Tags:    []string{"tag1", "tag2"},
		Numbers: []int{100, 200},
	}
	buf, err := MarshalNestedPayload(input, nil)
	if err != nil {
		t.Fatalf("MarshalNestedPayload failed: %v", err)
	}

	t.Run("unaligned Profile pointer offset", func(t *testing.T) {
		corrupted := bytes.Clone(buf)
		mirror := (*nestedPayloadLitzMirror)(unsafe.Pointer(&corrupted[8]))
		mirror.Profile += 1 // make it unaligned
		var out NestedPayload
		err := UnmarshalNestedPayload(corrupted, &out)
		if err != litz.ErrUnalignedBuffer {
			t.Fatalf("expected ErrUnalignedBuffer, got: %v", err)
		}
	})

	t.Run("unaligned Tags slice offset", func(t *testing.T) {
		corrupted := bytes.Clone(buf)
		mirror := (*nestedPayloadLitzMirror)(unsafe.Pointer(&corrupted[8]))
		mirror.Tags.offset += 1 // make it unaligned
		var out NestedPayload
		err := UnmarshalNestedPayload(corrupted, &out)
		if err != litz.ErrUnalignedBuffer {
			t.Fatalf("expected ErrUnalignedBuffer, got: %v", err)
		}
	})

	t.Run("unaligned Numbers primitive slice offset", func(t *testing.T) {
		corrupted := bytes.Clone(buf)
		mirror := (*nestedPayloadLitzMirror)(unsafe.Pointer(&corrupted[8]))
		mirror.Numbers.offset += 1 // make it unaligned
		var out NestedPayload
		err := UnmarshalNestedPayload(corrupted, &out)
		if err != litz.ErrUnalignedBuffer {
			t.Fatalf("expected ErrUnalignedBuffer, got: %v", err)
		}
	})
}

func TestCloneAlignedAndEnsureAligned(t *testing.T) {
	input := &UserProfile{Age: 45, Score: 88.8, Name: "AlignmentHelperTest", Active: true}
	buf, err := MarshalUserProfile(input, nil)
	if err != nil {
		t.Fatalf("MarshalUserProfile failed: %v", err)
	}

	// Create an unaligned slice by slicing at odd offset
	raw := make([]byte, len(buf)+3)
	copy(raw[3:], buf)
	unaligned := raw[3:]

	if uintptr(unsafe.Pointer(&unaligned[0]))%8 == 0 {
		unaligned = raw[1 : 1+len(buf)]
	}

	// 1. Verify unmarshaling directly fails
	var out UserProfile
	if err := UnmarshalUserProfile(unaligned, &out); err != litz.ErrUnalignedBuffer {
		t.Fatalf("expected ErrUnalignedBuffer for unaligned slice, got: %v", err)
	}

	// 2. CloneAligned
	cloned := litz.CloneAligned(unaligned)
	if uintptr(unsafe.Pointer(&cloned[0]))%8 != 0 {
		t.Fatalf("CloneAligned did not return 8-byte aligned buffer")
	}
	if err := UnmarshalUserProfile(cloned, &out); err != nil {
		t.Fatalf("UnmarshalUserProfile on CloneAligned failed: %v", err)
	}
	if out.Name != "AlignmentHelperTest" || out.Age != 45 {
		t.Fatalf("decoded content mismatch: %+v", out)
	}

	// 3. EnsureAligned on unaligned input
	ensured := litz.EnsureAligned(unaligned)
	if uintptr(unsafe.Pointer(&ensured[0]))%8 != 0 {
		t.Fatalf("EnsureAligned did not return 8-byte aligned buffer for unaligned input")
	}
	if err := UnmarshalUserProfile(ensured, &out); err != nil {
		t.Fatalf("UnmarshalUserProfile on EnsureAligned failed: %v", err)
	}

	// 4. EnsureAligned on already aligned input (must return identical slice without reallocation)
	alignedOrig := litz.AlignedBuffer(len(buf))
	copy(alignedOrig, buf)
	ensuredSame := litz.EnsureAligned(alignedOrig)
	if unsafe.SliceData(ensuredSame) != unsafe.SliceData(alignedOrig) {
		t.Fatalf("EnsureAligned reallocated already aligned buffer")
	}

	// 5. Edge cases: nil and empty
	if litz.CloneAligned(nil) != nil {
		t.Fatalf("CloneAligned(nil) should return nil")
	}
	if litz.CloneAligned([]byte{}) != nil {
		t.Fatalf("CloneAligned(empty) should return nil")
	}
	if litz.EnsureAligned(nil) != nil {
		t.Fatalf("EnsureAligned(nil) should return nil")
	}
	if litz.EnsureAligned([]byte{}) != nil {
		t.Fatalf("EnsureAligned(empty) should return nil")
	}
}

func TestPoolDiscardsUnalignedBuffer(t *testing.T) {
	p := litz.NewPool(256)
	b := p.Get(128)
	if uintptr(unsafe.Pointer(unsafe.SliceData(*b)))%8 != 0 {
		t.Fatalf("Pool.Get did not return aligned buffer")
	}

	// Create an unaligned buffer
	raw := make([]byte, 256)
	unaligned := raw[1:]
	p.Put(&unaligned)

	// Future Get should still return an aligned buffer
	b2 := p.Get(128)
	if uintptr(unsafe.Pointer(unsafe.SliceData(*b2)))%8 != 0 {
		t.Fatalf("Pool.Get returned unaligned buffer after unaligned Put")
	}
}

func TestSliceSwizzleAlignmentSafety(t *testing.T) {
	aligned := litz.AlignedBuffer(64)
	for i := range aligned {
		aligned[i] = byte(i)
	}

	// Aligned offset for uint64 (offset 8)
	s := litz.SliceSwizzle[uint64](aligned, 8, 4)
	if s == nil || len(s) != 4 {
		t.Fatalf("expected valid uint64 slice for aligned offset")
	}

	// Unaligned offset for uint64 (offset 9)
	sUnaligned := litz.SliceSwizzle[uint64](aligned, 9, 4)
	if sUnaligned != nil {
		t.Fatalf("expected nil for unaligned uint64 offset")
	}
}

func TestUnmarshalZeroesOutputStructAndPreventsStaleFields(t *testing.T) {
	// 1. Simulate an older serialized payload that only contains Age and Score (24 bytes).
	buf24 := litz.EnsureAligned(make([]byte, 24))
	*(*[4]byte)(unsafe.Pointer(&buf24[0])) = [4]byte{'L', 'T', 'Z', '0'}
	buf24[4] = 1
	*(*uint32)(unsafe.Pointer(&buf24[8])) = 42
	*(*float64)(unsafe.Pointer(&buf24[16])) = 3.14

	// Reuse a struct with dirty/stale fields
	output := UserProfile{
		Age:    999,
		Score:  888.8,
		Name:   "STALE_OLD_NAME",
		Active: true,
	}

	if err := UnmarshalUserProfile(buf24, &output); err != nil {
		t.Fatalf("UnmarshalUserProfile failed: %v", err)
	}
	if output.Age != 42 {
		t.Errorf("Expected Age=42, got %d", output.Age)
	}
	if output.Score != 3.14 {
		t.Errorf("Expected Score=3.14, got %f", output.Score)
	}
	if output.Name != "" {
		t.Errorf("Expected Name to be reset to empty, got %q", output.Name)
	}
	if output.Active != false {
		t.Errorf("Expected Active to be reset to false, got %t", output.Active)
	}

	// 2. An even shorter 12-byte payload that only contains Age = 7
	buf12 := litz.EnsureAligned(make([]byte, 12))
	*(*[4]byte)(unsafe.Pointer(&buf12[0])) = [4]byte{'L', 'T', 'Z', '0'}
	buf12[4] = 1
	*(*uint32)(unsafe.Pointer(&buf12[8])) = 7

	output = UserProfile{Age: 100, Score: 50.5, Name: "STALE", Active: true}
	if err := UnmarshalUserProfile(buf12, &output); err != nil {
		t.Fatalf("UnmarshalUserProfile(buf12) failed: %v", err)
	}
	if output.Age != 7 || output.Score != 0 || output.Name != "" || output.Active != false {
		t.Errorf("Expected only Age=7 and all others reset to zero, got %+v", output)
	}

	// 3. Header-only payload (8 bytes) must reset everything to zero
	buf8 := litz.EnsureAligned([]byte{'L', 'T', 'Z', '0', 1, 0, 0, 0})
	output = UserProfile{Age: 100, Score: 50.5, Name: "STALE", Active: true}
	if err := UnmarshalUserProfile(buf8, &output); err != nil {
		t.Fatalf("UnmarshalUserProfile(buf8) failed: %v", err)
	}
	if output != (UserProfile{}) {
		t.Errorf("Expected entire struct to be zeroed on header-only payload, got %+v", output)
	}
}

func TestUnmarshalRejectsNilDestination(t *testing.T) {
	buf := litz.EnsureAligned([]byte{'L', 'T', 'Z', '0', 1, 0, 0, 0})

	if err := UnmarshalUserProfile(buf, nil); err != litz.ErrNilDestination {
		t.Errorf("expected ErrNilDestination, got %v", err)
	}
	if err := UnmarshalUserProfileStrict(buf, nil); err != litz.ErrNilDestination {
		t.Errorf("expected ErrNilDestination for strict, got %v", err)
	}
	if err := UnmarshalSingleKey(buf, nil); err != litz.ErrNilDestination {
		t.Errorf("expected ErrNilDestination for SingleKey, got %v", err)
	}
	if err := UnmarshalSingleKeyStrict(buf, nil); err != litz.ErrNilDestination {
		t.Errorf("expected ErrNilDestination for SingleKeyStrict, got %v", err)
	}
	if err := UnmarshalNestedPayload(buf, nil); err != litz.ErrNilDestination {
		t.Errorf("expected ErrNilDestination for NestedPayload, got %v", err)
	}
	if err := UnmarshalNestedPayloadStrict(buf, nil); err != litz.ErrNilDestination {
		t.Errorf("expected ErrNilDestination for NestedPayloadStrict, got %v", err)
	}
}

func TestTableDrivenPayloadTruncation(t *testing.T) {
	testCases := []struct {
		name          string
		marshalFull   func() ([]byte, error)
		unmarshal     func([]byte) error
		unmarshStrict func([]byte) error
	}{
		{
			name: "UserProfile",
			marshalFull: func() ([]byte, error) {
				return MarshalUserProfile(&UserProfile{Age: 30, Score: 95.5, Name: "TableDrivenTest", Active: true}, nil)
			},
			unmarshal: func(b []byte) error {
				var u UserProfile
				return UnmarshalUserProfile(b, &u)
			},
			unmarshStrict: func(b []byte) error {
				var u UserProfile
				return UnmarshalUserProfileStrict(b, &u)
			},
		},
		{
			name: "SingleKey",
			marshalFull: func() ([]byte, error) {
				return MarshalSingleKey(&SingleKey{ID: 99999}, nil)
			},
			unmarshal: func(b []byte) error {
				var u SingleKey
				return UnmarshalSingleKey(b, &u)
			},
			unmarshStrict: func(b []byte) error {
				var u SingleKey
				return UnmarshalSingleKeyStrict(b, &u)
			},
		},
		{
			name: "NestedPayload",
			marshalFull: func() ([]byte, error) {
				return MarshalNestedPayload(&NestedPayload{
					Profile:   &UserProfile{Age: 25, Name: "NestedUser"},
					Tags:      []string{"tag1", "tag2"},
					Numbers:   []int{1, 2, 3},
					Signature: []byte("sign"),
				}, nil)
			},
			unmarshal: func(b []byte) error {
				var u NestedPayload
				return UnmarshalNestedPayload(b, &u)
			},
			unmarshStrict: func(b []byte) error {
				var u NestedPayload
				return UnmarshalNestedPayloadStrict(b, &u)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fullBuf, err := tc.marshalFull()
			if err != nil {
				t.Fatalf("marshalFull failed: %v", err)
			}

			// Cut the payload at every single byte from 0 to len-1
			for i := 0; i < len(fullBuf); i++ {
				sub := fullBuf[:i]

				// Unmarshal (evolutionary) must never panic
				errEvolution := tc.unmarshal(sub)
				if i < 8 && errEvolution != litz.ErrBufferTooShort {
					t.Fatalf("truncation at %d: expected ErrBufferTooShort, got %v", i, errEvolution)
				}

				// UnmarshalStrict must never panic AND must always reject truncated payload
				errStrict := tc.unmarshStrict(sub)
				if errStrict == nil {
					t.Fatalf("truncation at %d: UnmarshalStrict expected error on truncated buffer, got nil", i)
				}
			}
		})
	}
}

func TestUnmarshalXStrictSuccessOnFullPayload(t *testing.T) {
	input := &UserProfile{Age: 33, Score: 88.5, Name: "StrictModeUser", Active: true}
	buf, err := MarshalUserProfile(input, nil)
	if err != nil {
		t.Fatalf("MarshalUserProfile failed: %v", err)
	}

	var output UserProfile
	if err := UnmarshalUserProfileStrict(buf, &output); err != nil {
		t.Fatalf("UnmarshalUserProfileStrict failed on full payload: %v", err)
	}
	if !reflect.DeepEqual(*input, output) {
		t.Fatalf("StrictMode decoded mismatch:\nexpected: %+v\ngot: %+v", *input, output)
	}

	// SingleKey
	skInput := &SingleKey{ID: 777}
	skBuf, err := MarshalSingleKey(skInput, nil)
	if err != nil {
		t.Fatalf("MarshalSingleKey failed: %v", err)
	}
	var skOutput SingleKey
	if err := UnmarshalSingleKeyStrict(skBuf, &skOutput); err != nil {
		t.Fatalf("UnmarshalSingleKeyStrict failed: %v", err)
	}
	if skOutput.ID != 777 {
		t.Fatalf("SingleKeyStrict mismatch: %d", skOutput.ID)
	}
}

func TestNestedChildHeaderBoundsCheck(t *testing.T) {
	// OuterMessage has SeqNum uint64, Body *NestedPayload
	input := &OuterMessage{
		SeqNum: 101,
		Body: &NestedPayload{
			Profile: &UserProfile{Age: 20, Name: "Child"},
		},
	}
	buf, err := MarshalOuterMessage(input, nil)
	if err != nil {
		t.Fatalf("MarshalOuterMessage failed: %v", err)
	}

	// Find the child offset in mirror
	mirror := (*outerMessageLitzMirror)(unsafe.Pointer(&buf[8]))
	childOffset := mirror.Body
	if childOffset == 0 {
		t.Fatalf("expected non-zero child offset")
	}

	// Truncate buf so that child offset starts, but has fewer than 8 + sizeof(childMirror) bytes remaining.
	// For example, childOffset + 8 (only child header without mirror)
	truncated := buf[:childOffset+8]
	var out OuterMessage
	err = UnmarshalOuterMessage(truncated, &out)
	if err != litz.ErrPointerOutOfBounds {
		t.Fatalf("expected ErrPointerOutOfBounds for truncated child header, got: %v", err)
	}
}

func TestCrossArchitectureCompilation(t *testing.T) {
	tests := []struct {
		name        string
		goarch      string
		expectPass  bool
		errContains string
	}{
		{name: "amd64 (supported 64-bit LE)", goarch: "amd64", expectPass: true},
		{name: "arm64 (supported 64-bit LE)", goarch: "arm64", expectPass: true},
		{name: "riscv64 (supported 64-bit LE)", goarch: "riscv64", expectPass: true},
		{name: "ppc64le (supported 64-bit LE)", goarch: "ppc64le", expectPass: true},
		{
			name:        "386 (unsupported 32-bit)",
			goarch:      "386",
			expectPass:  false,
			errContains: "build constraints exclude all Go files",
		},
		{
			name:        "arm (unsupported 32-bit)",
			goarch:      "arm",
			expectPass:  false,
			errContains: "build constraints exclude all Go files",
		},
		{
			name:        "s390x (unsupported big-endian)",
			goarch:      "s390x",
			expectPass:  false,
			errContains: "build constraints exclude all Go files",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("go", "build", "-o", os.DevNull, "github.com/cuprite-io/litz")
			cmd.Env = append(os.Environ(), "GOARCH="+tc.goarch)
			out, err := cmd.CombinedOutput()

			if tc.expectPass {
				if err != nil {
					t.Fatalf("expected build to succeed for GOARCH=%s, got error: %v\nOutput: %s", tc.goarch, err, string(out))
				}
			} else {
				if err == nil {
					t.Fatalf("expected build to fail for GOARCH=%s, but it succeeded", tc.goarch)
				}
				if !strings.Contains(string(out), tc.errContains) {
					t.Fatalf("expected error output to contain %q, got:\n%s", tc.errContains, string(out))
				}
			}
		})
	}
}

func TestGoldenPayloadWireFormat(t *testing.T) {
	// Verify SingleKey wire format exactly
	sk := &SingleKey{ID: 0x0102030405060708}
	skBuf, err := MarshalSingleKey(sk, nil)
	if err != nil {
		t.Fatalf("MarshalSingleKey failed: %v", err)
	}

	expectedSK := []byte{
		'L', 'T', 'Z', '0', 1, 0, 0, 0, // Header
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, // ID (little-endian uint64)
	}
	if !bytes.Equal(skBuf, expectedSK) {
		t.Fatalf("SingleKey golden mismatch:\nexpected: %x\ngot:      %x", expectedSK, skBuf)
	}

	// Verify UserProfile golden wire bytes
	up := &UserProfile{
		Age:    0x11223344,
		Score:  1.5,
		Name:   "Hi",
		Active: true,
	}
	upBuf, err := MarshalUserProfile(up, nil)
	if err != nil {
		t.Fatalf("MarshalUserProfile failed: %v", err)
	}

	// Expected UserProfile wire layout on 64-bit LE:
	// 0..8:   Header 'LTZ0\x01\x00\x00\x00'
	// 8..12:  Age uint32 = 0x44, 0x33, 0x22, 0x11
	// 12..16: Struct alignment padding = 0, 0, 0, 0
	// 16..24: Score float64 (1.5) = 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf8, 0x3f
	// 24..32: Name.offset uintptr = 48 (0x30, 0, 0, 0, 0, 0, 0, 0)
	// 32..40: Name.length int = 2 (2, 0, 0, 0, 0, 0, 0, 0)
	// 40..41: Active bool = 1
	// 41..48: Padding = 0, 0, 0, 0, 0, 0, 0
	// 48..50: Name string bytes = 'H', 'i'
	// 50..56: 8-byte variable padding = 0, 0, 0, 0, 0, 0
	expectedUP := []byte{
		'L', 'T', 'Z', '0', 1, 0, 0, 0,
		0x44, 0x33, 0x22, 0x11, 0, 0, 0, 0,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf8, 0x3f,
		48, 0, 0, 0, 0, 0, 0, 0,
		2, 0, 0, 0, 0, 0, 0, 0,
		1, 0, 0, 0, 0, 0, 0, 0,
		'H', 'i', 0, 0, 0, 0, 0, 0,
	}

	if !bytes.Equal(upBuf, expectedUP) {
		t.Fatalf("UserProfile golden mismatch:\nexpected (%d bytes): %x\ngot      (%d bytes): %x",
			len(expectedUP), expectedUP, len(upBuf), upBuf)
	}

	// Unmarshal and assert roundtrip
	var out UserProfile
	if err := UnmarshalUserProfile(upBuf, &out); err != nil {
		t.Fatalf("UnmarshalUserProfile failed on golden wire: %v", err)
	}
	if !reflect.DeepEqual(*up, out) {
		t.Fatalf("roundtrip mismatch:\nexpected: %+v\ngot:      %+v", *up, out)
	}
}

func TestHIBI_4GiB_OverflowClamping(t *testing.T) {
	t.Run("AlignedBufferBounds", func(t *testing.T) {
		if buf := litz.AlignedBuffer(-1); buf != nil {
			t.Errorf("AlignedBuffer(-1) expected nil, got len %d", len(buf))
		}
		if buf := litz.AlignedBuffer(0); buf != nil {
			t.Errorf("AlignedBuffer(0) expected nil, got len %d", len(buf))
		}
		if buf := litz.AlignedBuffer(math.MaxInt); buf != nil {
			t.Errorf("AlignedBuffer(math.MaxInt) expected nil, got len %d", len(buf))
		}
	})

	t.Run("DynamicStringEmpty", func(t *testing.T) {
		dNil := litz.NewDynamic(nil, litz.TypeString)
		if s := dNil.String(); s != "" {
			t.Errorf("dNil.String() expected empty, got %q", s)
		}
		dEmpty := litz.NewDynamic([]byte{}, litz.TypeString)
		if s := dEmpty.String(); s != "" {
			t.Errorf("dEmpty.String() expected empty, got %q", s)
		}
	})

	t.Run("MapBoundsAndUnderflow", func(t *testing.T) {
		validMap := map[string]any{
			"alpha": "value_alpha",
			"beta":  "value_beta",
		}
		buf, typ, err := litz.MarshalAny(validMap)
		if err != nil {
			t.Fatalf("MarshalAny failed: %v", err)
		}
		dyn := litz.NewDynamic(buf, typ)
		if _, ok := dyn.GetView("alpha"); !ok {
			t.Fatalf("GetView(alpha) failed on valid buffer")
		}

		// Corrupt entry 0 offset so valOffset < keyLen
		corruptUnderflow := append([]byte(nil), buf...)
		// Entry 0 offset is at bytes 4..20 (entryOffset = 4).
		// valOffset is at entryOffset+4 = 8.
		// Set valOffset = 1, while keyLen is 5 ("alpha").
		*(*uint32)(unsafe.Pointer(&corruptUnderflow[8])) = 1
		dynCorrupt := litz.NewDynamic(corruptUnderflow, typ)
		if keys := dynCorrupt.Keys(); keys != nil {
			t.Errorf("Keys() expected nil on offset underflow, got %v", keys)
		}
		if _, ok := dynCorrupt.GetView("alpha"); ok {
			t.Errorf("GetView() expected false on offset underflow")
		}

		// Corrupt entry 0 so keyStart points into index table (e.g. valOffset = 5, keyLen = 5 -> keyStart = 0 < indexSize)
		corruptIndexOverlap := append([]byte(nil), buf...)
		*(*uint32)(unsafe.Pointer(&corruptIndexOverlap[8])) = 5
		dynOverlap := litz.NewDynamic(corruptIndexOverlap, typ)
		if keys := dynOverlap.Keys(); keys != nil {
			t.Errorf("Keys() expected nil on index overlap, got %v", keys)
		}
		if _, ok := dynOverlap.GetView("alpha"); ok {
			t.Errorf("GetView() expected false on index overlap")
		}

		// Corrupt entry 0 valLength so valEnd > len(buf)
		corruptValEnd := append([]byte(nil), buf...)
		// valLength is at entryOffset+8 = 12.
		*(*uint32)(unsafe.Pointer(&corruptValEnd[12])) = 0x7FFFFFFF
		dynValEnd := litz.NewDynamic(corruptValEnd, typ)
		if _, ok := dynValEnd.GetView("alpha"); ok {
			t.Errorf("GetView() expected false on valEnd > len(buf)")
		}

		// Corrupt entry 0 valOffset so valStart > valEnd (integer overflow in addition)
		corruptWrap := append([]byte(nil), buf...)
		*(*uint32)(unsafe.Pointer(&corruptWrap[8])) = 0xFFFFFFF0
		*(*uint32)(unsafe.Pointer(&corruptWrap[12])) = 0x20
		dynWrap := litz.NewDynamic(corruptWrap, typ)
		if _, ok := dynWrap.GetView("alpha"); ok {
			t.Errorf("GetView() expected false on valStart > valEnd wrap")
		}
	})

	t.Run("MapCollisionCorruptedSibling", func(t *testing.T) {
		fooHash := litz.HashKey("foo")
		// Map layout: 2 entries with hash = fooHash
		// Entry 0 (mid=0): "foo", valid
		// Entry 1 (mid=1): corrupt sibling with valOffset < keyLen
		buf := make([]byte, 60)
		*(*uint32)(unsafe.Pointer(&buf[0])) = 2 // 2 entries

		// Entry 0
		*(*uint32)(unsafe.Pointer(&buf[4])) = fooHash // hash
		*(*uint32)(unsafe.Pointer(&buf[8])) = 40      // valOffset
		*(*uint32)(unsafe.Pointer(&buf[12])) = 4     // valLength
		buf[16] = litz.TypeString                     // valType
		buf[17] = 3                                   // keyLen: "foo"
		copy(buf[37:40], "foo")
		copy(buf[40:44], "val1")

		// Entry 1 - Corrupt this sibling with valOffset < keyLen
		*(*uint32)(unsafe.Pointer(&buf[20])) = fooHash // hash
		*(*uint32)(unsafe.Pointer(&buf[24])) = 1       // valOffset < keyLen
		*(*uint32)(unsafe.Pointer(&buf[28])) = 4       // valLength
		buf[32] = litz.TypeString                      // valType
		buf[33] = 3                                    // keyLen: "bar"

		dyn := litz.NewDynamic(buf, litz.TypeMap)
		view, ok := dyn.GetView("foo")
		if !ok || view.String() != "val1" {
			t.Errorf("GetView(foo) failed with corrupted collision sibling: got (%q, %t)", view.String(), ok)
		}

		// Now test when mid lands on corrupt entry 0 and valid entry 1 has "foo"
		buf2 := make([]byte, 60)
		*(*uint32)(unsafe.Pointer(&buf2[0])) = 2 // 2 entries
		// Entry 0 (mid=0) is corrupt
		*(*uint32)(unsafe.Pointer(&buf2[4])) = fooHash
		*(*uint32)(unsafe.Pointer(&buf2[8])) = 1 // valOffset < keyLen
		*(*uint32)(unsafe.Pointer(&buf2[12])) = 4
		buf2[16] = litz.TypeString
		buf2[17] = 3
		// Entry 1 is valid "foo"
		*(*uint32)(unsafe.Pointer(&buf2[20])) = fooHash
		*(*uint32)(unsafe.Pointer(&buf2[24])) = 40
		*(*uint32)(unsafe.Pointer(&buf2[28])) = 4
		buf2[32] = litz.TypeString
		buf2[33] = 3
		copy(buf2[37:40], "foo")
		copy(buf2[40:44], "val2")

		dyn2 := litz.NewDynamic(buf2, litz.TypeMap)
		view2, ok2 := dyn2.GetView("foo")
		if !ok2 || view2.String() != "val2" {
			t.Errorf("GetView(foo) failed when mid is corrupted sibling: got (%q, %t)", view2.String(), ok2)
		}
	})

	t.Run("SliceBoundsAndUnderflow", func(t *testing.T) {
		validSlice := []any{"elem1", "elem2"}
		buf, typ, err := litz.MarshalAny(validSlice)
		if err != nil {
			t.Fatalf("MarshalAny failed: %v", err)
		}
		dyn := litz.NewDynamic(buf, typ)
		slice := dyn.Slice()
		if len(slice) != 2 {
			t.Fatalf("Slice() expected len 2, got %d", len(slice))
		}

		// Corrupt entry 0 offset so valStart points into index table (< indexSize = 8 + 2*8 = 24)
		corruptUnderflow := append([]byte(nil), buf...)
		*(*uint32)(unsafe.Pointer(&corruptUnderflow[8])) = 10
		dynUnderflow := litz.NewDynamic(corruptUnderflow, typ)
		if res := dynUnderflow.Slice(); res != nil {
			t.Errorf("Slice() expected nil on index overlap, got %v", res)
		}

		// Corrupt entry 0 length so valEnd > len(buf)
		corruptEnd := append([]byte(nil), buf...)
		*(*uint32)(unsafe.Pointer(&corruptEnd[12])) = 0x7FFFFFFF
		dynEnd := litz.NewDynamic(corruptEnd, typ)
		if res := dynEnd.Slice(); res != nil {
			t.Errorf("Slice() expected nil on valEnd > len(buf), got %v", res)
		}

		// Corrupt entry 0 offset and length so valStart > valEnd
		corruptWrap := append([]byte(nil), buf...)
		*(*uint32)(unsafe.Pointer(&corruptWrap[8])) = 0xFFFFFFF0
		*(*uint32)(unsafe.Pointer(&corruptWrap[12])) = 0x20
		dynWrap := litz.NewDynamic(corruptWrap, typ)
		if res := dynWrap.Slice(); res != nil {
			t.Errorf("Slice() expected nil on valStart > valEnd, got %v", res)
		}
	})
}
