package benchmark

import (
	"testing"

	"github.com/cuprite-io/litz"
)

var (
	dynamicStringSink string
	dynamicAnySink    any
)

func newDynamicBenchmarkMap(tb testing.TB) *litz.Dynamic {
	tb.Helper()
	buf, _, err := litz.MarshalAny(map[string]any{
		"env":      "production",
		"version":  int64(42),
		"ratio":    float64(0.975),
		"verified": true,
		"tags":     []string{"one", "two", "three"},
	})
	if err != nil {
		tb.Fatal(err)
	}
	return litz.NewDynamic(buf, litz.TypeMap)
}

func BenchmarkDynamicGet(b *testing.B) {
	d := newDynamicBenchmarkMap(b)
	b.ReportAllocs()

	for b.Loop() {
		val := d.Get("env")
		if val == nil {
			b.Fatal("missing key")
		}
		dynamicStringSink = val.String()
	}
}

func BenchmarkDynamicGetView(b *testing.B) {
	d := newDynamicBenchmarkMap(b)
	b.ReportAllocs()

	for b.Loop() {
		val, ok := d.GetView("env")
		if !ok {
			b.Fatal("missing key")
		}
		dynamicStringSink = val.String()
	}
}

func BenchmarkDynamicInterfaceMap(b *testing.B) {
	d := newDynamicBenchmarkMap(b)
	b.ReportAllocs()

	for b.Loop() {
		dynamicAnySink = d.Interface()
	}
}
