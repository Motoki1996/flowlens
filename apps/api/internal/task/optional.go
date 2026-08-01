package task

import "github.com/flowlens/api/internal/optional"

// Optional and Present are aliases for the shared internal/optional types.
// The type moved out of this package once backlogs needed it too — see the
// package comment there for why it can't live here.
type Optional[T any] = optional.Optional[T]

// Present builds a set Optional; see optional.Present.
func Present[T any](v T) Optional[T] { return optional.Present(v) }
