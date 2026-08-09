//go:build amd64 || arm64 || 386 || arm || wasm || mips64le || ppc64le || riscv64

package litz

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// Version is the current version of the Litz serialization library.
const Version = "v0.1.5"

// HIBI Type Constants
const (
	TypeNull uint8 = iota
	TypeInt
	TypeFloat
	TypeBool
	TypeString
	TypeBytes
	TypeMap
	TypeSlice
	TypeUint
)

// Sentinel Errors to avoid heap allocations on error paths
var (
	ErrBufferTooShort     = errors.New("litz.Unmarshal: buffer too short for fixed part")
	ErrStringOutOfBounds  = errors.New("litz.Unmarshal: string out of bounds")
	ErrSliceOutOfBounds   = errors.New("litz.Unmarshal: slice out of bounds")
	ErrPointerOutOfBounds = errors.New("litz.Unmarshal: nested pointer out of bounds")
	ErrSizeOverflow       = errors.New("litz.Marshal: size integer overflow")
	ErrInvalidHeader      = errors.New("litz.Unmarshal: invalid format signature or version")
	ErrInvalidHIBIType    = errors.New("litz.Dynamic: invalid type for this operation")
)

// AlignedBuffer allocates a byte slice. Go's runtime allocator aligns heap
// allocations to 8-byte boundaries automatically for sizes >= 8 bytes.
func AlignedBuffer(size int) []byte {
	if size <= 0 {
		return nil
	}
	u64Slice := make([]uint64, (size+7)/8)
	return unsafe.Slice((*byte)(unsafe.Pointer(&u64Slice[0])), len(u64Slice)*8)[:size]
}

// Pool is a wrapper around sync.Pool for reusing serialization buffers.
type Pool struct {
	pool        sync.Pool
	initialSize int
}

func NewPool(initialSize int) *Pool {
	if initialSize <= 0 {
		initialSize = 2048
	}
	return &Pool{
		initialSize: initialSize,
		pool: sync.Pool{
			New: func() any {
				buf := AlignedBuffer(initialSize)
				return &buf
			},
		},
	}
}

func (p *Pool) Get(minSize int) *[]byte {
	bufPtr := p.pool.Get().(*[]byte)
	if cap(*bufPtr) < minSize {
		newSize := minSize
		if minSize < 1024*1024 { // Up to 1MB, double size
			newSize = minSize * 2
		}
		newBuf := AlignedBuffer(newSize)
		return &newBuf
	}
	*bufPtr = (*bufPtr)[:minSize]
	return bufPtr
}

// Put returns a buffer back to the pool.
// To prevent memory bloat during massive payload spikes, buffers with capacity
// larger than 16MB are discarded rather than returned to the pool.
// Note: We accept and return *[]byte (pointer to slice header) rather than []byte
// to prevent the Go runtime from allocating interface boxing containers on sync.Pool.Put,
// maintaining true zero-allocation execution on recycled paths.
func (p *Pool) Put(bufPtr *[]byte) {
	if bufPtr == nil || cap(*bufPtr) > 16*1024*1024 {
		return
	}
	p.pool.Put(bufPtr)
}

// HashKey computes FNV-1a 32-bit hash for a string key.
func HashKey(key string) uint32 {
	var hash uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= 16777619
	}
	return hash
}

// Dynamic represents unstructured schema-less data backed by raw bytes.
// It implements the Hash-Indexed Binary Index (HIBI) protocol.
type Dynamic struct {
	buf     []byte
	valType uint8
}

// NewDynamic creates a new Dynamic reader wrapping the given HIBI buffer and type code.
func NewDynamic(buf []byte, valType uint8) *Dynamic {
	return &Dynamic{buf: buf, valType: valType}
}

func (d *Dynamic) IsNil() bool {
	return d == nil || len(d.buf) == 0
}

func (d *Dynamic) Raw() []byte {
	if d.IsNil() {
		return nil
	}
	return d.buf
}

func (d *Dynamic) Type() uint8 {
	if d.IsNil() {
		return TypeNull
	}
	return d.valType
}

func (d *Dynamic) Len() int {
	if d.IsNil() {
		return 0
	}
	if d.valType == TypeMap || d.valType == TypeSlice {
		if len(d.buf) < 4 {
			return 0
		}
		return int(*(*uint32)(unsafe.Pointer(&d.buf[0])))
	}
	return len(d.buf)
}

// Keys returns all keys present in the HIBI map
func (d *Dynamic) Keys() []string {
	if d.IsNil() || d.valType != TypeMap || len(d.buf) < 4 {
		return nil
	}
	numEntries := int(*(*uint32)(unsafe.Pointer(&d.buf[0])))
	maxEntries := (len(d.buf) - 4) / 16
	if numEntries > maxEntries {
		return nil
	}
	keys := make([]string, numEntries)
	for i := 0; i < numEntries; i++ {
		entryOffset := 4 + i*16
		if entryOffset+16 > len(d.buf) {
			return nil
		}
		entryPtr := unsafe.Pointer(&d.buf[entryOffset])
		valOffset := *(*uint32)(unsafe.Add(entryPtr, 4))
		keyLen := *(*uint8)(unsafe.Add(entryPtr, 13))
		keyStart := int(valOffset) - int(keyLen)
		if keyStart < 4 || int(valOffset) > len(d.buf) {
			return nil
		}
		keys[i] = unsafe.String((*byte)(unsafe.Pointer(&d.buf[keyStart])), int(keyLen))
	}
	return keys
}

// Get performs an O(log N) binary search lookup for a key inside a HIBI map.
// Crucially, it resolves hash collisions by verifying the full key string.
func (d *Dynamic) Get(key string) *Dynamic {
	val, _ := d.GetOK(key)
	return val
}

// GetOK is like Get, but also returns a boolean indicating whether the key was found.
func (d *Dynamic) GetOK(key string) (*Dynamic, bool) {
	if d.IsNil() || d.valType != TypeMap || len(d.buf) < 4 {
		return nil, false
	}
	numEntries := *(*uint32)(unsafe.Pointer(&d.buf[0]))
	if numEntries == 0 || numEntries > math.MaxInt32 {
		return nil, false
	}
	maxEntries := (len(d.buf) - 4) / 16
	if int(numEntries) > maxEntries {
		return nil, false
	}
	hash := HashKey(key)

	// Binary search on the index table which starts at offset 4.
	// Each entry is 16 bytes: Hash (4B), ValOffset (4B), ValLength (4B), ValType (1B), KeyLength (1B), Padding (2B).
	low := 0
	high := int(numEntries) - 1
	for low <= high {
		mid := (low + high) >> 1
		entryOffset := 4 + mid*16
		if entryOffset+16 > len(d.buf) {
			return nil, false
		}
		entryPtr := unsafe.Pointer(&d.buf[entryOffset])
		entryHash := *(*uint32)(entryPtr)

		if entryHash == hash {
			// Read metadata
			valOffset := *(*uint32)(unsafe.Add(entryPtr, 4))
			valLength := *(*uint32)(unsafe.Add(entryPtr, 8))
			valType := *(*uint8)(unsafe.Add(entryPtr, 12))
			keyLen := *(*uint8)(unsafe.Add(entryPtr, 13))

			// Key is stored immediately before the value: starting at valOffset - keyLen
			keyStart := int(valOffset) - int(keyLen)
			if keyStart < 4 || int(valOffset) > len(d.buf) {
				return nil, false
			}

			// Verify if key matches (Hash collision verification)
			entryKey := unsafe.String((*byte)(unsafe.Pointer(&d.buf[keyStart])), int(keyLen))
			if entryKey == key {
				valStart := int(valOffset)
				valEnd := valStart + int(valLength)
				if valEnd > len(d.buf) {
					return nil, false
				}
				return &Dynamic{
					buf:     d.buf[valStart:valEnd],
					valType: valType,
				}, true
			}

			// In case of a hash collision, keys with duplicate hashes are sorted adjacent
			// Scan left for same hash keys
			for l := mid - 1; l >= 0; l-- {
				lOffset := 4 + l*16
				lPtr := unsafe.Pointer(&d.buf[lOffset])
				if *(*uint32)(lPtr) != hash {
					break
				}
				lValOffset := *(*uint32)(unsafe.Add(lPtr, 4))
				lValLength := *(*uint32)(unsafe.Add(lPtr, 8))
				lValType := *(*uint8)(unsafe.Add(lPtr, 12))
				lKeyLen := *(*uint8)(unsafe.Add(lPtr, 13))
				lKeyStart := int(lValOffset) - int(lKeyLen)
				lValEnd := int(lValOffset) + int(lValLength)
				if lKeyStart >= 4 && lValEnd <= len(d.buf) && int(lValOffset) <= lValEnd {
					lKey := unsafe.String((*byte)(unsafe.Pointer(&d.buf[lKeyStart])), int(lKeyLen))
					if lKey == key {
						return &Dynamic{
							buf:     d.buf[int(lValOffset):lValEnd],
							valType: lValType,
						}, true
					}
				}
			}
			// Scan right for same hash keys
			for r := mid + 1; r < int(numEntries); r++ {
				rOffset := 4 + r*16
				if rOffset+16 > len(d.buf) {
					break
				}
				rPtr := unsafe.Pointer(&d.buf[rOffset])
				if *(*uint32)(rPtr) != hash {
					break
				}
				rValOffset := *(*uint32)(unsafe.Add(rPtr, 4))
				rValLength := *(*uint32)(unsafe.Add(rPtr, 8))
				rValType := *(*uint8)(unsafe.Add(rPtr, 12))
				rKeyLen := *(*uint8)(unsafe.Add(rPtr, 13))
				rKeyStart := int(rValOffset) - int(rKeyLen)
				rValEnd := int(rValOffset) + int(rValLength)
				if rKeyStart >= 4 && rValEnd <= len(d.buf) && int(rValOffset) <= rValEnd {
					rKey := unsafe.String((*byte)(unsafe.Pointer(&d.buf[rKeyStart])), int(rKeyLen))
					if rKey == key {
						return &Dynamic{
							buf:     d.buf[int(rValOffset):rValEnd],
							valType: rValType,
						}, true
					}
				}
			}
			return nil, false
		} else if entryHash < hash {
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return nil, false
}

// Int returns the value as an int64.
func (d *Dynamic) Int() int64 {
	if d.IsNil() || d.valType != TypeInt || len(d.buf) < 8 {
		return 0
	}
	return *(*int64)(unsafe.Pointer(&d.buf[0]))
}

// Uint returns the value as a uint64.
func (d *Dynamic) Uint() uint64 {
	if d.IsNil() || d.valType != TypeUint || len(d.buf) < 8 {
		return 0
	}
	return *(*uint64)(unsafe.Pointer(&d.buf[0]))
}

// Float returns the value as a float64.
func (d *Dynamic) Float() float64 {
	if d.IsNil() || d.valType != TypeFloat || len(d.buf) < 8 {
		return 0.0
	}
	return *(*float64)(unsafe.Pointer(&d.buf[0]))
}

// Bool returns the value as a bool.
func (d *Dynamic) Bool() bool {
	if d.IsNil() || d.valType != TypeBool || len(d.buf) < 1 {
		return false
	}
	return d.buf[0] != 0
}

// String returns the value as a string (zero-copy pointer casting).
func (d *Dynamic) String() string {
	if d.IsNil() || d.valType != TypeString {
		return ""
	}
	return unsafe.String(&d.buf[0], len(d.buf))
}

// Bytes returns the underlying byte slice.
func (d *Dynamic) Bytes() []byte {
	if d.IsNil() || d.valType != TypeBytes {
		return nil
	}
	return d.buf
}

// Map converts the HIBI payload into a standard Go map[string]*Dynamic.
// Validates that this Dynamic object is actually a Map.
func (d *Dynamic) Map(keys []string) map[string]*Dynamic {
	if d.IsNil() || d.valType != TypeMap {
		return nil
	}
	m := make(map[string]*Dynamic, len(keys))
	for _, key := range keys {
		if val := d.Get(key); val != nil {
			m[key] = val
		}
	}
	return m
}

// Slice returns the dynamic elements if this Dynamic object is a slice.
// Validates that this Dynamic object is actually a Slice and resolves
// the dynamic element type from the slice header.
func (d *Dynamic) Slice() []*Dynamic {
	if d.IsNil() || d.valType != TypeSlice || len(d.buf) < 8 {
		return nil
	}
	numElements := *(*uint32)(unsafe.Pointer(&d.buf[0]))
	if numElements == 0 || numElements > math.MaxInt32 {
		return nil
	}
	maxElements := (len(d.buf) - 8) / 8
	if int(numElements) > maxElements {
		return nil
	}
	// Read the element type from offset 4 (as written by marshalSlice)
	eltType := d.buf[4]

	slice := make([]*Dynamic, numElements)
	for i := 0; i < int(numElements); i++ {
		entryOffset := 8 + i*8 // index table starts after 8-byte slice header
		if entryOffset+8 > len(d.buf) {
			return nil
		}
		entryPtr := unsafe.Pointer(&d.buf[entryOffset])
		offset := *(*uint32)(entryPtr)
		length := *(*uint32)(unsafe.Add(entryPtr, 4))

		valStart := int(offset)
		valEnd := valStart + int(length)
		if valEnd > len(d.buf) || valStart > valEnd {
			return nil
		}
		slice[i] = &Dynamic{buf: d.buf[valStart:valEnd], valType: eltType}
	}
	return slice
}

// CloneAny performs a deep copy of common interface{} types to prevent use-after-free.
func CloneAny(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case *Dynamic:
		if val == nil {
			return nil
		}
		b := make([]byte, len(val.buf))
		copy(b, val.buf)
		return &Dynamic{buf: b, valType: val.valType}
	case string:
		return strings.Clone(val)
	case []byte:
		b := make([]byte, len(val))
		copy(b, val)
		return b
	case map[string]any:
		m := make(map[string]any, len(val))
		for k, valItem := range val {
			m[k] = CloneAny(valItem)
		}
		return m
	default:
		// Fallback for slices, structs, primitives
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Slice {
			resSlice := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
			for i := 0; i < rv.Len(); i++ {
				clonedVal := CloneAny(rv.Index(i).Interface())
				if clonedVal != nil {
					resSlice.Index(i).Set(reflect.ValueOf(clonedVal))
				}
			}
			return resSlice.Interface()
		}
		return v
	}
}

// hibiKeyValue holds temporary data for serialization sorting
type hibiKeyValue struct {
	hash    uint32
	key     string
	valType uint8
	bytes   []byte
}

// MarshalAny serializes any Go value into the HIBI format.
// Returns the serialized bytes, type identifier, and error.
func MarshalAny(v any) ([]byte, uint8, error) {
	if v == nil {
		return []byte{}, TypeNull, nil
	}

	switch val := v.(type) {
	case int:
		buf := AlignedBuffer(8)
		*(*int64)(unsafe.Pointer(&buf[0])) = int64(val)
		return buf, TypeInt, nil
	case int64:
		buf := AlignedBuffer(8)
		*(*int64)(unsafe.Pointer(&buf[0])) = val
		return buf, TypeInt, nil
	case int32:
		buf := AlignedBuffer(8)
		*(*int64)(unsafe.Pointer(&buf[0])) = int64(val)
		return buf, TypeInt, nil
	case int16:
		buf := AlignedBuffer(8)
		*(*int64)(unsafe.Pointer(&buf[0])) = int64(val)
		return buf, TypeInt, nil
	case int8:
		buf := AlignedBuffer(8)
		*(*int64)(unsafe.Pointer(&buf[0])) = int64(val)
		return buf, TypeInt, nil
	case uint64:
		buf := AlignedBuffer(8)
		*(*uint64)(unsafe.Pointer(&buf[0])) = val
		return buf, TypeUint, nil
	case uint:
		buf := AlignedBuffer(8)
		*(*uint64)(unsafe.Pointer(&buf[0])) = uint64(val)
		return buf, TypeUint, nil
	case uint32:
		buf := AlignedBuffer(8)
		*(*uint64)(unsafe.Pointer(&buf[0])) = uint64(val)
		return buf, TypeUint, nil
	case uint16:
		buf := AlignedBuffer(8)
		*(*uint64)(unsafe.Pointer(&buf[0])) = uint64(val)
		return buf, TypeUint, nil
	case uint8:
		buf := AlignedBuffer(8)
		*(*uint64)(unsafe.Pointer(&buf[0])) = uint64(val)
		return buf, TypeUint, nil
	case float64:
		buf := AlignedBuffer(8)
		*(*float64)(unsafe.Pointer(&buf[0])) = val
		return buf, TypeFloat, nil
	case float32:
		buf := AlignedBuffer(8)
		*(*float64)(unsafe.Pointer(&buf[0])) = float64(val)
		return buf, TypeFloat, nil
	case bool:
		buf := AlignedBuffer(1)
		if val {
			buf[0] = 1
		}
		return buf, TypeBool, nil
	case string:
		buf := AlignedBuffer(len(val))
		copy(buf, val)
		return buf, TypeString, nil
	case []byte:
		buf := AlignedBuffer(len(val))
		copy(buf, val)
		return buf, TypeBytes, nil
	case time.Time:
		buf := AlignedBuffer(8)
		*(*int64)(unsafe.Pointer(&buf[0])) = val.UnixNano()
		return buf, TypeInt, nil
	case [16]byte:
		buf := AlignedBuffer(16)
		copy(buf, val[:])
		return buf, TypeBytes, nil
	case *Dynamic:
		if val == nil {
			return nil, TypeNull, nil
		}
		return val.buf, val.valType, nil
	case map[string]any:
		b, err := marshalMap(val)
		return b, TypeMap, err
	default:
		// Reflection fallback for other types (e.g. slices, structs)
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Map {
			if rv.Type().Key().Kind() != reflect.String {
				return nil, TypeNull, fmt.Errorf("litz.MarshalAny: unsupported map key type %s (only map[string]T is supported)", rv.Type().Key().Kind())
			}
			m := make(map[string]any)
			for _, key := range rv.MapKeys() {
				m[key.String()] = rv.MapIndex(key).Interface()
			}
			b, err := marshalMap(m)
			return b, TypeMap, err
		}
		if rv.Kind() == reflect.Slice {
			b, err := marshalSlice(rv)
			return b, TypeSlice, err
		}
		return nil, TypeNull, fmt.Errorf("litz.MarshalAny: unsupported type %T", v)
	}
}

func marshalMap(m map[string]any) ([]byte, error) {
	numEntries := len(m)
	if numEntries == 0 {
		buf := make([]byte, 4)
		*(*uint32)(unsafe.Pointer(&buf[0])) = 0
		return buf, nil
	}

	elements := make([]hibiKeyValue, 0, numEntries)
	for k, v := range m {
		if len(k) > 255 {
			return nil, fmt.Errorf("litz.marshalMap: key length %d exceeds maximum of 255 bytes", len(k))
		}
		valBytes, valType, err := MarshalAny(v)
		if err != nil {
			return nil, err
		}
		elements = append(elements, hibiKeyValue{
			hash:    HashKey(k),
			key:     k,
			valType: valType,
			bytes:   valBytes,
		})
	}

	// Sort by hash to enable O(log N) binary search
	sort.Slice(elements, func(i, j int) bool {
		return elements[i].hash < elements[j].hash
	})

	if numEntries > (math.MaxInt-4)/16 {
		return nil, errors.New("litz.marshalMap: map size overflows maximum elements")
	}
	indexSize := 4 + numEntries*16
	payloadSize := 0
	for _, el := range elements {
		payloadSize += len(el.key) + len(el.bytes)
	}

	totalSize := indexSize + payloadSize
	if totalSize < 0 || totalSize < indexSize || totalSize < payloadSize {
		return nil, errors.New("litz.marshalMap: map size overflows int")
	}
	buf := AlignedBuffer(totalSize)

	// Write number of entries
	*(*uint32)(unsafe.Pointer(&buf[0])) = uint32(numEntries)

	currentOffset := uint32(indexSize)
	for i, el := range elements {
		entryOffset := 4 + i*16
		entryPtr := unsafe.Pointer(&buf[entryOffset])

		keyLen := uint32(len(el.key))
		valLen := uint32(len(el.bytes))

		// Write Hash
		*(*uint32)(entryPtr) = el.hash
		// Write Offset (Value starts after key string)
		*(*uint32)(unsafe.Add(entryPtr, 4)) = currentOffset + keyLen
		// Write Length
		*(*uint32)(unsafe.Add(entryPtr, 8)) = valLen
		// Write Type
		*(*uint8)(unsafe.Add(entryPtr, 12)) = el.valType
		// Write KeyLength
		*(*uint8)(unsafe.Add(entryPtr, 13)) = uint8(keyLen)

		// Copy key string to payload
		copy(buf[currentOffset:currentOffset+keyLen], el.key)
		// Copy value bytes right after key
		copy(buf[currentOffset+keyLen:currentOffset+keyLen+valLen], el.bytes)

		currentOffset += keyLen + valLen
	}

	return buf, nil
}

func marshalSlice(rv reflect.Value) ([]byte, error) {
	numElements := rv.Len()
	if numElements == 0 {
		buf := make([]byte, 4)
		*(*uint32)(unsafe.Pointer(&buf[0])) = 0
		return buf, nil
	}

	// Prevent Integer Overflow
	if numElements > (math.MaxInt32-8)/8 {
		return nil, errors.New("litz.marshalSlice: slice length overflows maximum elements")
	}

	// Dynamic slice serialization:
	// Header: 8 bytes (4B element count, 1B element type, 3B padding)
	// Array: numElements * 8B entries (4B offset, 4B length)
	// Followed by raw payloads
	indexSize := 8 + numElements*8
	payloads := make([][]byte, numElements)
	var eltType uint8 = TypeNull
	totalPayloadSize := 0
	for i := 0; i < numElements; i++ {
		b, t, err := MarshalAny(rv.Index(i).Interface())
		if err != nil {
			return nil, err
		}
		if i == 0 {
			eltType = t
		} else if t != eltType {
			return nil, fmt.Errorf("litz.marshalSlice: heterogeneous slice element type %d does not match first element type %d", t, eltType)
		}
		payloads[i] = b
		totalPayloadSize += len(b)
	}

	totalSize := indexSize + totalPayloadSize
	if totalSize < 0 || totalSize < indexSize || totalSize < totalPayloadSize {
		return nil, errors.New("litz.marshalSlice: slice size overflows int")
	}
	buf := AlignedBuffer(totalSize)

	// Write element count
	*(*uint32)(unsafe.Pointer(&buf[0])) = uint32(numElements)
	// Write element type
	buf[4] = eltType

	currentOffset := uint32(indexSize)

	for i, p := range payloads {
		entryOffset := 8 + i*8
		entryPtr := unsafe.Pointer(&buf[entryOffset])

		*(*uint32)(entryPtr) = currentOffset
		*(*uint32)(unsafe.Add(entryPtr, 4)) = uint32(len(p))

		copy(buf[currentOffset:currentOffset+uint32(len(p))], p)
		currentOffset += uint32(len(p))
	}

	return buf, nil
}

// Helper methods for swizzling pointer offsets in code generated code

// StringSwizzle converts an offset in buf to a valid Go string.
// WARNING: The returned string points directly into the buffer memory.
// The buffer MUST outlive the returned string to avoid use-after-free corruption.
func StringSwizzle(buf []byte, offset uintptr, length int) string {
	if length <= 0 || offset > uintptr(len(buf)) || length > len(buf)-int(offset) {
		return ""
	}
	return unsafe.String((*byte)(unsafe.Add(unsafe.Pointer(&buf[0]), offset)), length)
}

// StringSwizzleUnchecked is an unchecked variant of StringSwizzle.
// Re-uses direct string backing arrays without runtime bounds checking.
// WARNING: Calling this with a corrupted or malicious offset will trigger a segmentation fault or memory read violation.
func StringSwizzleUnchecked(buf []byte, offset uintptr, length int) string {
	if length <= 0 {
		return ""
	}
	return unsafe.String((*byte)(unsafe.Add(unsafe.Pointer(&buf[0]), offset)), length)
}

// SliceSwizzle converts an offset in buf to a valid Go slice.
// WARNING: The returned slice points directly into the buffer memory.
func SliceSwizzle[T any](buf []byte, offset uintptr, length int) []T {
	if length <= 0 {
		return nil
	}
	var empty T
	elementSize := int(unsafe.Sizeof(empty))
	if offset > uintptr(len(buf)) || length > (len(buf)-int(offset))/elementSize {
		return nil
	}
	return unsafe.Slice((*T)(unsafe.Add(unsafe.Pointer(&buf[0]), offset)), length)
}

// Interface converts the Dynamic value back to a standard Go interface representation.
func (d *Dynamic) Interface() any {
	if d.IsNil() {
		return nil
	}
	switch d.valType {
	case TypeNull:
		return nil
	case TypeInt:
		return d.Int()
	case TypeUint:
		return d.Uint()
	case TypeFloat:
		return d.Float()
	case TypeBool:
		return d.Bool()
	case TypeString:
		return d.String()
	case TypeBytes:
		return d.Bytes()
	case TypeMap:
		keys := d.Keys()
		m := make(map[string]any, len(keys))
		for _, k := range keys {
			val := d.Get(k)
			if val != nil {
				m[k] = val.Interface()
			}
		}
		return m
	case TypeSlice:
		elements := d.Slice()
		s := make([]any, len(elements))
		for i, el := range elements {
			if el != nil {
				s[i] = el.Interface()
			}
		}
		return s
	default:
		return nil
	}
}

// ToMap converts the Dynamic object back to a standard Go map[string]any.
// Returns an error if the underlying value is not a HIBI map.
func (d *Dynamic) ToMap() (map[string]any, error) {
	if d.IsNil() || d.valType != TypeMap {
		return nil, fmt.Errorf("litz.Dynamic: value type %d is not a map", d.valType)
	}
	return d.Interface().(map[string]any), nil
}

// ToSlice converts the Dynamic object back to a standard Go []any.
// Returns an error if the underlying value is not a HIBI slice.
func (d *Dynamic) ToSlice() ([]any, error) {
	if d.IsNil() || d.valType != TypeSlice {
		return nil, fmt.Errorf("litz.Dynamic: value type %d is not a slice", d.valType)
	}
	return d.Interface().([]any), nil
}
