package benchmark

// SmallLitzPayload represents the small benchmark structure
//litz:generate
type SmallLitzPayload struct {
	ID uint64
}

// MediumLitzPayload represents the medium benchmark structure
//litz:generate
type MediumLitzPayload struct {
	ID     uint64
	Email  string
	Active bool
	Clicks uint32
}

// LargeLitzPayload represents the large benchmark structure
//litz:generate
type LargeLitzPayload struct {
	ID      uint64
	Name    string
	Active  bool
	Numbers []int64
	Tags    []string
	Child   *MediumLitzPayload
}
