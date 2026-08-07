package benchmark

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/proto"
)

// Global variables to prevent compiler optimization
var (
	globalLitzBuf []byte
	globalProtoBuf []byte
	globalMsgpBuf []byte
	globalJSONBuf []byte
)

// Benchmarking instances
var (
	smallLitzInput = SmallLitzPayload{ID: 12345}
	smallProtoInput = SmallPayload{Id: 12345}

	mediumLitzInput = MediumLitzPayload{
		ID:     54321,
		Email:  "user@example.com",
		Active: true,
		Clicks: 9999,
	}
	mediumProtoInput = MediumPayload{
		Id:     54321,
		Email:  "user@example.com",
		Active: true,
		Clicks: 9999,
	}

	largeLitzInput = LargeLitzPayload{
		ID:      11111,
		Name:    "Large Test Payload with Multiple Fields and Slices",
		Active:  true,
		Numbers: []int64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
		Tags:    []string{"latency", "throughput", "serialization", "benchmark", "litz"},
		Child: &MediumLitzPayload{
			ID:     8888,
			Email:  "child@domain.com",
			Active: false,
			Clicks: 12,
		},
	}
	largeProtoInput = LargePayload{
		Id:      11111,
		Name:    "Large Test Payload with Multiple Fields and Slices",
		Active:  true,
		Numbers: []int64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
		Tags:    []string{"latency", "throughput", "serialization", "benchmark", "litz"},
		Child: &MediumPayload{
			Id:     8888,
			Email:  "child@domain.com",
			Active: false,
			Clicks: 12,
		},
	}
)

// ==========================================
// SMALL PAYLOAD BENCHMARKS
// ==========================================

func BenchmarkMarshalLitz_Small(b *testing.B) {
	buf := make([]byte, 128)
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = MarshalSmallLitzPayload(&smallLitzInput, buf)
		if err != nil {
			b.Fatal(err)
		}
	}
	globalLitzBuf = r
}

func BenchmarkUnmarshalLitz_Small(b *testing.B) {
	buf, _ := MarshalSmallLitzPayload(&smallLitzInput, nil)
	var out SmallLitzPayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := UnmarshalSmallLitzPayload(buf, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalProto_Small(b *testing.B) {
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = proto.Marshal(&smallProtoInput)
		if err != nil {
			b.Fatal(err)
		}
	}
	globalProtoBuf = r
}

func BenchmarkUnmarshalProto_Small(b *testing.B) {
	buf, _ := proto.Marshal(&smallProtoInput)
	var out SmallPayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := proto.Unmarshal(buf, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalVtProto_Small(b *testing.B) {
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = smallProtoInput.MarshalVT()
		if err != nil {
			b.Fatal(err)
		}
	}
	globalProtoBuf = r
}

func BenchmarkUnmarshalVtProto_Small(b *testing.B) {
	buf, _ := smallProtoInput.MarshalVT()
	var out SmallPayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := out.UnmarshalVT(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalMsgp_Small(b *testing.B) {
	buf := make([]byte, 0, 128)
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = smallLitzInput.MarshalMsg(buf[:0])
		if err != nil {
			b.Fatal(err)
		}
	}
	globalMsgpBuf = r
}

func BenchmarkUnmarshalMsgp_Small(b *testing.B) {
	buf, _ := smallLitzInput.MarshalMsg(nil)
	var out SmallLitzPayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := out.UnmarshalMsg(buf); err != nil {
			b.Fatal(err)
		}
	}
}

// ==========================================
// MEDIUM PAYLOAD BENCHMARKS
// ==========================================

func BenchmarkMarshalLitz_Medium(b *testing.B) {
	buf := make([]byte, 512)
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = MarshalMediumLitzPayload(&mediumLitzInput, buf)
		if err != nil {
			b.Fatal(err)
		}
	}
	globalLitzBuf = r
}

func BenchmarkUnmarshalLitz_Medium(b *testing.B) {
	buf, _ := MarshalMediumLitzPayload(&mediumLitzInput, nil)
	var out MediumLitzPayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := UnmarshalMediumLitzPayload(buf, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalProto_Medium(b *testing.B) {
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = proto.Marshal(&mediumProtoInput)
		if err != nil {
			b.Fatal(err)
		}
	}
	globalProtoBuf = r
}

func BenchmarkUnmarshalProto_Medium(b *testing.B) {
	buf, _ := proto.Marshal(&mediumProtoInput)
	var out MediumPayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := proto.Unmarshal(buf, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalVtProto_Medium(b *testing.B) {
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = mediumProtoInput.MarshalVT()
		if err != nil {
			b.Fatal(err)
		}
	}
	globalProtoBuf = r
}

func BenchmarkUnmarshalVtProto_Medium(b *testing.B) {
	buf, _ := mediumProtoInput.MarshalVT()
	var out MediumPayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := out.UnmarshalVT(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalMsgp_Medium(b *testing.B) {
	buf := make([]byte, 0, 512)
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = mediumLitzInput.MarshalMsg(buf[:0])
		if err != nil {
			b.Fatal(err)
		}
	}
	globalMsgpBuf = r
}

func BenchmarkUnmarshalMsgp_Medium(b *testing.B) {
	buf, _ := mediumLitzInput.MarshalMsg(nil)
	var out MediumLitzPayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := out.UnmarshalMsg(buf); err != nil {
			b.Fatal(err)
		}
	}
}

// ==========================================
// LARGE PAYLOAD BENCHMARKS
// ==========================================

func BenchmarkMarshalLitz_Large(b *testing.B) {
	buf := make([]byte, 2048)
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = MarshalLargeLitzPayload(&largeLitzInput, buf)
		if err != nil {
			b.Fatal(err)
		}
	}
	globalLitzBuf = r
}

func BenchmarkUnmarshalLitz_Large(b *testing.B) {
	buf, _ := MarshalLargeLitzPayload(&largeLitzInput, nil)
	var out LargeLitzPayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := UnmarshalLargeLitzPayload(buf, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalProto_Large(b *testing.B) {
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = proto.Marshal(&largeProtoInput)
		if err != nil {
			b.Fatal(err)
		}
	}
	globalProtoBuf = r
}

func BenchmarkUnmarshalProto_Large(b *testing.B) {
	buf, _ := proto.Marshal(&largeProtoInput)
	var out LargePayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := proto.Unmarshal(buf, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalVtProto_Large(b *testing.B) {
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = largeProtoInput.MarshalVT()
		if err != nil {
			b.Fatal(err)
		}
	}
	globalProtoBuf = r
}

func BenchmarkUnmarshalVtProto_Large(b *testing.B) {
	buf, _ := largeProtoInput.MarshalVT()
	var out LargePayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := out.UnmarshalVT(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalMsgp_Large(b *testing.B) {
	buf := make([]byte, 0, 2048)
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = largeLitzInput.MarshalMsg(buf[:0])
		if err != nil {
			b.Fatal(err)
		}
	}
	globalMsgpBuf = r
}

func BenchmarkUnmarshalMsgp_Large(b *testing.B) {
	buf, _ := largeLitzInput.MarshalMsg(nil)
	var out LargeLitzPayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := out.UnmarshalMsg(buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalJSONStd_Large(b *testing.B) {
	var r []byte
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		r, err = json.Marshal(&largeLitzInput)
		if err != nil {
			b.Fatal(err)
		}
	}
	globalJSONBuf = r
}

func BenchmarkUnmarshalJSONStd_Large(b *testing.B) {
	buf, _ := json.Marshal(&largeLitzInput)
	var out LargeLitzPayload
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := json.Unmarshal(buf, &out); err != nil {
			b.Fatal(err)
		}
	}
}
