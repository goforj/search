package search_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/goforj/search"
)

// TestAsyncObserverBoundedFIFO proves capacity, nonblocking admission, ordered delivery, and shutdown behavior.
func TestAsyncObserverBoundedFIFO(t *testing.T) {
	started, unblock := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var kinds []string
	observer := search.ObserverFunc(func(_ context.Context, event search.Event) {
		once.Do(func() { close(started); <-unblock })
		kinds = append(kinds, event.Kind)
	})
	adapter, err := search.NewAsyncObserver(observer, 1)
	if err != nil {
		t.Fatal(err)
	}
	adapter.Observe(context.Background(), search.Event{Kind: "one"})
	<-started
	adapter.Observe(context.Background(), search.Event{Kind: "two"})
	adapter.Observe(context.Background(), search.Event{Kind: "three"})
	if adapter.Dropped() != 1 {
		t.Fatal(adapter.Dropped())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := adapter.Shutdown(ctx); err != context.Canceled {
		t.Fatal(err)
	}
	close(unblock)
	if err := adapter.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	adapter.Observe(context.Background(), search.Event{Kind: "four"})
	if adapter.Dropped() != 2 || !reflect.DeepEqual(kinds, []string{"one", "two"}) {
		t.Fatal(adapter.Dropped(), kinds)
	}
	for _, capacity := range []int{0, -1, 65537} {
		if _, err := search.NewAsyncObserver(observer, capacity); err == nil {
			t.Fatal("invalid capacity")
		}
	}
}
