package fanout

import (
	"sync"
	"time"

	"github.com/eloylp/kit/moment"

	guuid "github.com/google/uuid"
)

// Status represents a report about how much
// elements are queued per consumer.
type Status map[string]int

// CancelFunc main way that a consumer can end its
// subscribe. When called, the subscriber channel
// will be closed so consumer will still try  to
// consume all the remain messages.
type CancelFunc func() error

// subscriber is an internal representation of a
// consumer. The UUID can be used later for the
// Unsubscribe() method, in the case the consumer
// wants to gracefully stop consuming. Of course
// This UUID can be used for custom identification
// purposes.
//
// It also holds channel of Slots, that represent
// consumer data.
type subscriber struct {
	ch   chan *Slot
	UUID string
}

// Slot represents queueable element. Timestamp
// will allow consumers discard old messages. The
// type of Elem in an interface{} for the sake of
// flexibility.
type Slot struct {
	TimeStamp time.Time
	Elem      interface{}
}

// BufferedFanOut represent a fan-out pattern making
// use of channels.
// Will send a copy of the element to multiple
// subscribers at the same time.
// This implements the needed locking to be considered "goroutine safe".
type BufferedFanOut struct {
	subscribers []subscriber
	maxBuffLen  int
	L           sync.RWMutex
	Now         moment.Now
}

// NewBufferedFanOut needs buffer size for subscribers channels
// and function that must retrieve the current time for Slots
// timestamps.
func NewBufferedFanOut(maxBuffLen int, now moment.Now) *BufferedFanOut {
	fo := &BufferedFanOut{
		maxBuffLen: maxBuffLen,
		Now:        now,
	}
	return fo
}

// Add will send of elem to all subscribers channels.
// Take care about race conditions and the type of
// element that pass in. If you pass an integer, that
// will be copied to each subscriber. Problem comes if you
// pass referenced types like maps or slices or any other
// pointer type.
// If one of the subscribers channels is full, oldest data
// will be discarded.
func (fo *BufferedFanOut) Add(elem interface{}) {
	fo.L.Lock()
	defer fo.L.Unlock()
	sl := &Slot{
		TimeStamp: fo.Now(),
		Elem:      elem,
	}
	fo.publish(sl)
}

// SubscribersLen can tell us how many subscribers
// are registered in the present moment.
func (fo *BufferedFanOut) SubscribersLen() int {
	fo.L.RLock()
	defer fo.L.RUnlock()
	return len(fo.subscribers)
}

func (fo *BufferedFanOut) publish(sl *Slot) {
	for _, s := range fo.subscribers {
		if len(s.ch) == fo.maxBuffLen {
			<-s.ch // remove last Slot of subscriber channel
		}
		s.ch <- sl
	}
}

// Subscribe will return an output channel that will
// be filled when more data arrives this fanout. It
// will also return the associated UUID for a later
// Unsubscribe() operation. Also a cancelFunc, for
// easily canceling the subscription without the need of
// the UUID. This means that there are two ways of
// canceling a subscription.
//
// If you are not actively consuming this channel, but
// data continues arriving to the fanout, the oldest
// element will be dropped in favor of the new one.
func (fo *BufferedFanOut) Subscribe() (<-chan *Slot, string, CancelFunc) { //nolint:gocritic
	fo.L.Lock()
	defer fo.L.Unlock()
	ch := make(chan *Slot, fo.maxBuffLen)
	uuid := guuid.New().String()
	fo.subscribers = append(fo.subscribers, subscriber{ch, uuid})
	return ch, uuid, func() error {
		return fo.Unsubscribe(uuid)
	}
}

// Unsubscribe will require the UUID obtained via a
// Subscribe() operation to properly clear all resources.
func (fo *BufferedFanOut) Unsubscribe(uuid string) error {
	fo.L.Lock()
	defer fo.L.Unlock()
	if !fo.exists(uuid) {
		return ErrSubscriberNotFound
	}
	newSubs := make([]subscriber, 0, len(fo.subscribers))
	for _, s := range fo.subscribers {
		if s.UUID == uuid {
			close(s.ch)
		} else {
			newSubs = append(newSubs, s)
		}
	}
	fo.subscribers = newSubs
	return nil
}

func (fo *BufferedFanOut) exists(uuid string) bool {
	for _, s := range fo.subscribers {
		if s.UUID == uuid {
			return true
		}
	}
	return false
}

// Reset will clear all the data and
// subscribers, starting again. It will
// also close all the subscribers channels.
func (fo *BufferedFanOut) Reset() {
	fo.L.Lock()
	defer fo.L.Unlock()
	for _, s := range fo.subscribers {
		close(s.ch)
	}
	fo.subscribers = nil
}

// Status will return a Status type with
// the list of all subscribers and they
// pending elements. Could be used to decide
// if we need a second fanout instance to
// properly process all the incoming data.
// This of course could be a decision made
// in run time.
func (fo *BufferedFanOut) Status() Status {
	fo.L.RLock()
	defer fo.L.RUnlock()
	status := make(Status, len(fo.subscribers))
	for _, s := range fo.subscribers {
		status[s.UUID] = len(s.ch)
	}
	return status
}
