package task

import "encoding/json"

// Optional distinguishes a field that was absent from a JSON request body
// from one that was present. This is what makes PATCH a real partial update:
// UpdateParams carries Optional fields, and Update leaves every absent one at
// its current value instead of overwriting it with a zero.
//
// A nullable field is spelled Optional[*T]: absent leaves it alone, an
// explicit `null` clears it, a value sets it. That three-state distinction is
// why a plain *T is not enough here.
type Optional[T any] struct {
	set   bool
	value T
}

// Present builds a set Optional. Handlers never need it — encoding/json sets
// the flag — but tests and callers constructing UpdateParams directly do.
func Present[T any](v T) Optional[T] {
	return Optional[T]{set: true, value: v}
}

// Get returns the value and whether it was set.
func (o Optional[T]) Get() (T, bool) {
	return o.value, o.set
}

// Or returns the value when set, and fallback otherwise.
func (o Optional[T]) Or(fallback T) T {
	if !o.set {
		return fallback
	}
	return o.value
}

// UnmarshalJSON marks the field as set. encoding/json calls this only for
// keys actually present in the body, including those explicitly `null` — for
// Optional[*T] a `null` unmarshals to a nil pointer, which is exactly the
// "clear this field" signal.
func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.set = true
	return json.Unmarshal(b, &o.value)
}
