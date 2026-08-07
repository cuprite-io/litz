package litz_test

//litz:generate
type SingleKey struct {
	ID uint64
}

//litz:generate
type UserProfile struct {
	Age    uint32
	Score  float64
	Name   string
	Active bool
}

//litz:generate
type NestedPayload struct {
	Profile   *UserProfile
	Metadata  any // Dynamic mapping
	Tags      []string
	Numbers   []int
	Signature []byte
}

//litz:generate
type OuterMessage struct {
	SeqNum uint64
	Body   *NestedPayload
}
