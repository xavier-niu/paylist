package server

import (
	"context"
	"sync"
	"time"
)

// agentIdleTimeout closes a turn that has made no progress (no text, no tool
// call) for this long: a model that stopped answering.
var agentIdleTimeout = 5 * time.Minute

// An agent turn runs on its own, apart from the request that started it: the
// page can be left, closed or reloaded and the turn goes on, saving the
// conversation as it goes. Whoever is watching (the page that started it, or
// one that opens the conversation later) gets the turn's events from its
// start, then as they come.

// agentRun is one turn underway.
type agentRun struct {
	userID    int64
	chatID    string
	turnStart int // messages in the conversation before this turn's replies
	cancel    context.CancelFunc

	mu     sync.Mutex
	events []any
	done   bool
	wake   chan struct{} // closed, and replaced, when something happens
	last   time.Time     // the last progress
	idled  bool          // closed for want of progress
}

func (r *agentRun) add(e any) {
	r.mu.Lock()
	r.last = time.Now()
	r.events = append(r.events, e)
	close(r.wake)
	r.wake = make(chan struct{})
	r.mu.Unlock()
}

func (r *agentRun) finish() {
	r.mu.Lock()
	r.done = true
	close(r.wake)
	r.wake = make(chan struct{})
	r.mu.Unlock()
}

// watch closes the turn when it goes agentIdleTimeout without progress, until
// it ends.
func (r *agentRun) watch(ctx context.Context) {
	tick := time.NewTicker(min(agentIdleTimeout/4, 15*time.Second))
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.mu.Lock()
			idle := time.Since(r.last) >= agentIdleTimeout
			if idle {
				r.idled = true
			}
			r.mu.Unlock()
			if idle {
				r.cancel()
				return
			}
		}
	}
}

func (r *agentRun) wasIdle() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.idled
}

// follow sends the turn's events from the first, then each as it comes,
// until the turn ends or the watcher goes.
func (r *agentRun) follow(ctx context.Context, send func(any) error) {
	i := 0
	for {
		r.mu.Lock()
		evs, done, wake := r.events[i:], r.done, r.wake
		r.mu.Unlock()
		for _, e := range evs {
			if send(e) != nil {
				return
			}
		}
		i += len(evs)
		if done {
			return
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return
		}
	}
}

// agentRuns are the turns underway, by conversation.
type agentRuns struct {
	mu     sync.Mutex
	byChat map[string]*agentRun
}

// start registers a turn for a conversation, unless one is already underway.
func (rs *agentRuns) start(r *agentRun) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.byChat == nil {
		rs.byChat = map[string]*agentRun{}
	}
	if _, busy := rs.byChat[r.chatID]; busy {
		return false
	}
	r.wake = make(chan struct{})
	r.last = time.Now()
	rs.byChat[r.chatID] = r
	return true
}

func (rs *agentRuns) end(r *agentRun) {
	rs.mu.Lock()
	if rs.byChat[r.chatID] == r {
		delete(rs.byChat, r.chatID)
	}
	rs.mu.Unlock()
	r.finish()
}

// get finds the turn underway in a user's conversation.
func (rs *agentRuns) get(userID int64, chatID string) *agentRun {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if r := rs.byChat[chatID]; r != nil && r.userID == userID {
		return r
	}
	return nil
}
