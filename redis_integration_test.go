package litz_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/cuprite-io/litz"
	"github.com/redis/go-redis/v9"
)

func TestRedisLitzIntegration(t *testing.T) {
	// Connect to local Redis server (redis-cli ping verified running)
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	ctx := context.Background()

	// Verify connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis is not available on localhost:6379, skipping integration test: %v", err)
	}
	defer rdb.Close()

	// Define varying sized payloads
	// 1. Small Payload
	smallPayload := &SingleKey{ID: 1024}

	// 2. Medium Payload
	mediumPayload := &UserProfile{
		Age:    28,
		Score:  98.5,
		Name:   "Litz Developer",
		Active: true,
	}

	// 3. Large Payload (Deeply nested with strings, slices, and dynamic metadata)
	largePayload := &OuterMessage{
		SeqNum: 99999,
		Body: &NestedPayload{
			Profile: &UserProfile{
				Age:    35,
				Score:  88.2,
				Name:   "Nested Senior Developer",
				Active: true,
			},
			Metadata: map[string]any{
				"env":    "production",
				"region": "us-east-1",
				"debug":  false,
			},
			Tags:      []string{"golang", "serialization", "high-performance"},
			Numbers:   []int{42, 137, 73},
			Signature: []byte{0xDE, 0xAD, 0xBE, 0xEF},
		},
	}

	// --- 1. Test Small Payload Store & Fetch ---
	t.Run("Small Payload", func(t *testing.T) {
		buf, err := MarshalSingleKey(smallPayload, nil)
		if err != nil {
			t.Fatalf("MarshalSingleKey failed: %v", err)
		}

		key := "litz:test:small"
		if err := rdb.Set(ctx, key, buf, 10*time.Second).Err(); err != nil {
			t.Fatalf("Redis SET failed: %v", err)
		}

		fetched, err := rdb.Get(ctx, key).Bytes()
		if err != nil {
			t.Fatalf("Redis GET failed: %v", err)
		}

		var output SingleKey
		if err := UnmarshalSingleKey(fetched, &output); err != nil {
			t.Fatalf("UnmarshalSingleKey failed: %v", err)
		}

		if output.ID != smallPayload.ID {
			t.Errorf("ID mismatch: expected %d, got %d", smallPayload.ID, output.ID)
		}
	})

	// --- 2. Test Medium Payload Store & Fetch ---
	t.Run("Medium Payload", func(t *testing.T) {
		buf, err := MarshalUserProfile(mediumPayload, nil)
		if err != nil {
			t.Fatalf("MarshalUserProfile failed: %v", err)
		}

		key := "litz:test:medium"
		if err := rdb.Set(ctx, key, buf, 10*time.Second).Err(); err != nil {
			t.Fatalf("Redis SET failed: %v", err)
		}

		fetched, err := rdb.Get(ctx, key).Bytes()
		if err != nil {
			t.Fatalf("Redis GET failed: %v", err)
		}

		var output UserProfile
		if err := UnmarshalUserProfile(fetched, &output); err != nil {
			t.Fatalf("UnmarshalUserProfile failed: %v", err)
		}

		if !reflect.DeepEqual(*mediumPayload, output) {
			t.Errorf("Medium payload mismatch. Expected %+v, got %+v", *mediumPayload, output)
		}
	})

	// --- 3. Test Large Payload Store & Fetch ---
	t.Run("Large Payload", func(t *testing.T) {
		buf, err := MarshalOuterMessage(largePayload, nil)
		if err != nil {
			t.Fatalf("MarshalOuterMessage failed: %v", err)
		}

		key := "litz:test:large"
		if err := rdb.Set(ctx, key, buf, 10*time.Second).Err(); err != nil {
			t.Fatalf("Redis SET failed: %v", err)
		}

		fetched, err := rdb.Get(ctx, key).Bytes()
		if err != nil {
			t.Fatalf("Redis GET failed: %v", err)
		}

		var output OuterMessage
		if err := UnmarshalOuterMessage(fetched, &output); err != nil {
			t.Fatalf("UnmarshalOuterMessage failed: %v", err)
		}

		// Verify deep structural equality
		if output.SeqNum != largePayload.SeqNum {
			t.Errorf("SeqNum mismatch: expected %d, got %d", largePayload.SeqNum, output.SeqNum)
		}

		if !reflect.DeepEqual(*largePayload.Body.Profile, *output.Body.Profile) {
			t.Errorf("Nested Profile mismatch")
		}

		if !reflect.DeepEqual(largePayload.Body.Tags, output.Body.Tags) {
			t.Errorf("Tags mismatch")
		}

		if !reflect.DeepEqual(largePayload.Body.Numbers, output.Body.Numbers) {
			t.Errorf("Numbers mismatch")
		}

		if !reflect.DeepEqual(largePayload.Body.Signature, output.Body.Signature) {
			t.Errorf("Signature mismatch")
		}

		// Verify dynamic metadata (maps)
		dynMap, ok := output.Body.Metadata.(*litz.Dynamic)
		if !ok || dynMap == nil {
			t.Fatalf("Expected metadata to be a *Dynamic map, got %T", output.Body.Metadata)
		}

		expectedMeta := largePayload.Body.Metadata.(map[string]any)
		for k, expectedVal := range expectedMeta {
			val := dynMap.Get(k)
			if val == nil {
				t.Fatalf("Expected key %q in dynamic metadata, got nil", k)
			}
			switch ev := expectedVal.(type) {
			case string:
				if val.String() != ev {
					t.Errorf("Metadata key %q mismatch: expected %q, got %q", k, ev, val.String())
				}
			case bool:
				if val.Bool() != ev {
					t.Errorf("Metadata key %q mismatch: expected %t, got %t", k, ev, val.Bool())
				}
			}
		}
	})
}
