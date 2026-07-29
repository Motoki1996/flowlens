package http

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSimpleRateLimiter_AllowsUpToLimitThenBlocks(t *testing.T) {
	l := newSimpleRateLimiter(2, time.Minute)

	assert.True(t, l.Allow("key"))
	assert.True(t, l.Allow("key"))
	assert.False(t, l.Allow("key"), "a third request within the same window must be blocked")
}

func TestSimpleRateLimiter_KeysAreIndependent(t *testing.T) {
	l := newSimpleRateLimiter(1, time.Minute)

	assert.True(t, l.Allow("a"))
	assert.True(t, l.Allow("b"), "a different key must have its own budget")
}

func TestSimpleRateLimiter_ResetsAfterWindow(t *testing.T) {
	l := newSimpleRateLimiter(1, time.Millisecond)

	assert.True(t, l.Allow("key"))
	time.Sleep(5 * time.Millisecond)
	assert.True(t, l.Allow("key"), "a new window must reset the count")
}
