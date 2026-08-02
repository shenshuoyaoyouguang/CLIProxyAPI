package redisqueue

import (
	"testing"
	"time"
)

func TestEnqueueBroadcastsToUsageSubscribersAndSkipsQueue(t *testing.T) {
	withEnabledQueue(t, func() {
		first, unsubscribeFirst := SubscribeUsage()
		defer unsubscribeFirst()
		second, unsubscribeSecond := SubscribeUsage()
		defer unsubscribeSecond()

		requireUsageSubscriberPayload(t, first, usageSupportRefreshPayload)
		requireUsageSubscriberPayload(t, second, usageSupportRefreshPayload)

		Enqueue([]byte("usage-record"))

		requireUsageSubscriberPayload(t, first, "usage-record")
		requireUsageSubscriberPayload(t, second, "usage-record")

		if items := PopOldest(1); len(items) != 0 {
			t.Fatalf("PopOldest() items = %q, want empty after subscriber broadcast", items)
		}

		unsubscribeFirst()
		unsubscribeSecond()

		Enqueue([]byte("queued-record"))
		items := PopOldest(1)
		if len(items) != 1 || string(items[0]) != "queued-record" {
			t.Fatalf("PopOldest() items = %q, want queued record after unsubscribe", items)
		}
	})
}

func TestSetEnabledFalseClosesUsageSubscribers(t *testing.T) {
	withEnabledQueue(t, func() {
		subscriber, unsubscribe := SubscribeUsage()
		defer unsubscribe()
		errorSubscriber, unsubscribeErrors := SubscribeErrors()
		defer unsubscribeErrors()

		requireUsageSubscriberPayload(t, subscriber, usageSupportRefreshPayload)

		SetEnabled(false)

		select {
		case _, ok := <-subscriber:
			if ok {
				t.Fatalf("subscriber channel remained open after SetEnabled(false)")
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for subscriber close")
		}

		select {
		case _, ok := <-errorSubscriber:
			if ok {
				t.Fatalf("error subscriber channel remained open after SetEnabled(false)")
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for error subscriber close")
		}
	})
}

func TestEnqueueErrorBroadcastsToErrorSubscribersAndDiscardsWithoutSubscribers(t *testing.T) {
	withEnabledQueue(t, func() {
		subscriber, unsubscribe := SubscribeErrors()
		defer unsubscribe()

		EnqueueError([]byte("error-record"))
		requireUsageSubscriberPayload(t, subscriber, "error-record")

		unsubscribe()

		EnqueueError([]byte("discarded-error"))
		requireErrorQueueEmpty(t)
	})
}

func TestNotifyUsageRefreshBroadcastsOnlyToUsageSubscribers(t *testing.T) {
	withEnabledQueue(t, func() {
		subscriber, unsubscribe := SubscribeUsage()
		defer unsubscribe()
		errorSubscriber, unsubscribeErrors := SubscribeErrors()
		defer unsubscribeErrors()

		requireUsageSubscriberPayload(t, subscriber, usageSupportRefreshPayload)

		NotifyUsageRefresh()
		requireUsageSubscriberPayload(t, subscriber, usageRefreshPayload)

		select {
		case got := <-errorSubscriber:
			t.Fatalf("error subscriber received usage refresh payload %q", string(got))
		default:
		}

		unsubscribe()
		NotifyUsageRefresh()
		if items := PopOldest(1); len(items) != 0 {
			t.Fatalf("PopOldest() items = %q, want empty after refresh notification without subscribers", items)
		}
	})
}

func TestEnqueueSlowSubscriberKeepsSubscriptionWhenBufferFull(t *testing.T) {
	withEnabledQueue(t, func() {
		// Subscribe with a tiny buffer so it fills quickly.
		slow, unsubscribeSlow := global.subscribe(1, nil)
		defer unsubscribeSlow()
		fast, unsubscribeFast := SubscribeUsage()
		defer unsubscribeFast()
		requireUsageSubscriberPayload(t, fast, usageSupportRefreshPayload)

		// Fill the slow subscriber's single-slot buffer. The fill is also
		// delivered to fast; drain it so the next read asserts the record.
		if !global.publishToSubscribers([]byte("fill-slow")) {
			t.Fatalf("publishToSubscribers() = false, want true for an accepting subscriber")
		}
		requireUsageSubscriberPayload(t, fast, "fill-slow")
		assertSubscriberBufferLen(t, slow, 1)

		// A record published while slow is full is dropped for slow but delivered
		// to fast, so Enqueue does not fall through to the queue.
		Enqueue([]byte("usage-record"))
		requireUsageSubscriberPayload(t, fast, "usage-record")
		if items := PopOldest(1); len(items) != 0 {
			t.Fatalf("PopOldest() items = %q, want empty when at least one subscriber accepted", items)
		}

		// The slow subscriber is still subscribed (not deleted) and its buffer is
		// still full: the record was dropped for it, not delivered.
		if got := subscriberCount(); got != 2 {
			t.Fatalf("subscriberCount() = %d, want 2 (slow subscriber must stay subscribed)", got)
		}
		assertSubscriberBufferLen(t, slow, 1)

		// Draining the slow subscriber delivers the fill payload, proving its
		// channel was not closed by the backpressure drop.
		requireUsageSubscriberPayload(t, slow, "fill-slow")

		// A new record is delivered to the slow subscriber after it drains.
		Enqueue([]byte("after-drain"))
		requireUsageSubscriberPayload(t, slow, "after-drain")

		// When no subscriber accepts (slow full again, fast unsubscribed), Enqueue
		// falls through to the queue instead of discarding the record.
		if !global.publishToSubscribers([]byte("fill-slow-2")) {
			t.Fatalf("publishToSubscribers() = false, want true for an accepting subscriber")
		}
		unsubscribeFast()
		assertSubscriberBufferLen(t, slow, 1)

		Enqueue([]byte("queued-record"))
		if got := subscriberCount(); got != 1 {
			t.Fatalf("subscriberCount() = %d, want 1 (slow subscriber must stay subscribed)", got)
		}
		if items := PopOldest(1); len(items) != 1 || string(items[0]) != "queued-record" {
			t.Fatalf("PopOldest() items = %q, want queued-record when no subscriber accepted", items)
		}
	})
}

func requireUsageSubscriberPayload(t *testing.T, subscriber <-chan []byte, want string) {
	t.Helper()

	select {
	case got, ok := <-subscriber:
		if !ok {
			t.Fatalf("subscriber closed before receiving %q", want)
		}
		if string(got) != want {
			t.Fatalf("subscriber payload = %q, want %q", string(got), want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for subscriber payload %q", want)
	}
}

func subscriberCount() int {
	global.mu.Lock()
	defer global.mu.Unlock()
	return len(global.subscribers)
}

func assertSubscriberBufferLen(t *testing.T, subscriber <-chan []byte, want int) {
	t.Helper()

	if got := len(subscriber); got != want {
		t.Fatalf("subscriber buffer len = %d, want %d", got, want)
	}
}

func requireErrorQueueEmpty(t *testing.T) {
	t.Helper()

	errorGlobal.mu.Lock()
	defer errorGlobal.mu.Unlock()

	if len(errorGlobal.items)-errorGlobal.head != 0 {
		t.Fatalf("error queue retained %d item(s), want none", len(errorGlobal.items)-errorGlobal.head)
	}
}
