package feed_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentrq/sql-mcp/backend/internal/service/feed"
)

func TestPublishReachesOnlyTheKeysSubscribers(t *testing.T) {
	h := feed.New[string]()
	a, cancelA := h.Subscribe(1, 4)
	b, cancelB := h.Subscribe(2, 4)
	defer cancelA()
	defer cancelB()

	h.Publish(1, "one")
	h.Publish(3, "nobody")

	assert.Equal(t, "one", <-a)
	assert.Empty(t, b)
}

func TestASlowSubscriberLosesEvents(t *testing.T) {
	h := feed.New[int]()
	ch, cancel := h.Subscribe(1, 1)
	defer cancel()

	h.Publish(1, 1)
	h.Publish(1, 2) // dropped, the buffer is full

	assert.Equal(t, 1, <-ch)
	assert.Empty(t, ch)
}

func TestCancelAndCloseEndSubscriptions(t *testing.T) {
	h := feed.New[int]()
	a, cancelA := h.Subscribe(1, 1)
	b, cancelB := h.Subscribe(1, 1)

	cancelA()
	cancelA() // a second cancel is harmless
	_, open := <-a
	assert.False(t, open)

	h.Close(1)
	_, open = <-b
	assert.False(t, open)
	cancelB() // after Close, cancel does not close it again

	h.Publish(1, 1) // no subscribers left
}
