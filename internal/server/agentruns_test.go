package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAgentRuns(t *testing.T) {
	var rs agentRuns
	ctx, cancel := context.WithCancel(context.Background())
	run := &agentRun{userID: 1, chatID: "abc", cancel: cancel}
	if !rs.start(run) {
		t.Fatal("first turn refused")
	}
	if rs.start(&agentRun{userID: 1, chatID: "abc"}) {
		t.Fatal("a second turn in the same conversation started")
	}
	if rs.get(2, "abc") != nil {
		t.Fatal("another user sees the turn")
	}
	run.add("chat")
	run.add("text 1")

	// One watcher from the start, following live.
	var mu sync.Mutex
	var live []any
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		run.follow(context.Background(), func(e any) error { mu.Lock(); live = append(live, e); mu.Unlock(); return nil })
	}()
	time.Sleep(20 * time.Millisecond)
	run.add("text 2")
	// One that leaves early stops following, and the turn goes on.
	gone, leave := context.WithCancel(context.Background())
	leave()
	run.follow(gone, func(any) error { return nil })
	run.add("done")
	rs.end(run)
	wg.Wait()
	if len(live) != 4 || live[0] != "chat" || live[3] != "done" {
		t.Fatalf("live: %v", live)
	}
	// One that comes after the end gets the whole turn, and stops.
	var late []any
	run.follow(context.Background(), func(e any) error { late = append(late, e); return nil })
	if len(late) != 4 {
		t.Fatalf("late: %v", late)
	}
	if rs.get(1, "abc") != nil || !rs.start(&agentRun{userID: 1, chatID: "abc"}) {
		t.Fatal("the conversation is still taken after its turn ended")
	}
	if ctx.Err() != nil {
		t.Fatal("ending a turn cancelled it")
	}
}

func TestAgentChatBackground(t *testing.T) {
	e := newEnv(t)
	c := e.signIn(t, "bg@example.com")
	events := readEvents(t, e.do(t, c, "POST", "/api/agent/chat", map[string]any{"message": "add Tea 8 Food Cash"}))
	id := events[0]["id"].(string)
	// With the turn over: no stream but its end, stopping does nothing, and nothing is listed as running.
	if got := readEvents(t, e.do(t, c, "GET", "/api/chats/"+id+"/stream", nil)); len(got) != 1 || got[0]["type"] != "done" {
		t.Errorf("stream after the turn: %v", got)
	}
	if code := status(t, e.do(t, c, "POST", "/api/chats/"+id+"/stop", map[string]any{})); code != 200 {
		t.Errorf("stop: %d", code)
	}
	chats := decodeJSON[[]map[string]any](t, e.do(t, c, "GET", "/api/chats", nil))
	if len(chats) != 1 || chats[0]["running"] != nil {
		t.Errorf("chats: %v", chats)
	}
	// Someone else's conversation: nothing to follow.
	other := e.signIn(t, "other@example.com")
	if got := readEvents(t, e.do(t, other, "GET", "/api/chats/"+id+"/stream", nil)); len(got) != 1 {
		t.Errorf("another user's stream: %v", got)
	}
	if code := status(t, e.do(t, &http.Client{}, "GET", "/api/chats/"+id+"/stream", nil)); code != 401 {
		t.Errorf("signed out: %d", code)
	}
}

// A turn goes on when its page goes, can be followed again from its start,
// and stops when asked.
func TestAgentChatRunsOn(t *testing.T) {
	e := newEnv(t)
	c := e.signIn(t, "slow@example.com")

	// Start a slow turn, read its first event, and leave.
	ctx, leave := context.WithCancel(context.Background())
	buf, _ := json.Marshal(map[string]any{"message": "wait 2"})
	req, _ := http.NewRequestWithContext(ctx, "POST", e.srv.URL+"/api/agent/chat", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var first map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&first); err != nil || first["type"] != "chat" {
		t.Fatalf("first event: %v %v", first, err)
	}
	id := first["id"].(string)
	leave()
	resp.Body.Close()

	// It's still running, and a second turn in it is refused.
	chat := decodeJSON[map[string]any](t, e.do(t, c, "GET", "/api/chats/"+id, nil))
	if chat["running"] != true || chat["turn_start"] != float64(1) {
		t.Fatalf("chat while running: %v", chat)
	}
	if code := status(t, e.do(t, c, "POST", "/api/agent/chat", map[string]any{"chat_id": id, "message": "hello"})); code != 409 {
		t.Errorf("second turn: %d", code)
	}
	// Following it again gets it from the start, to the end.
	events := readEvents(t, e.do(t, c, "GET", "/api/chats/"+id+"/stream", nil))
	var text strings.Builder
	for _, ev := range events {
		if ev["type"] == "text" {
			text.WriteString(ev["text"].(string))
		}
	}
	if events[0]["type"] != "chat" || events[len(events)-1]["type"] != "done" || !strings.Contains(text.String(), "Waited 2 seconds.") {
		t.Fatalf("followed: %v", events)
	}
	saved := decodeJSON[map[string]any](t, e.do(t, c, "GET", "/api/chats/"+id, nil))
	msgs := saved["messages"].([]any)
	if saved["running"] != nil || len(msgs) != 2 || !strings.Contains(msgs[1].(map[string]any)["text"].(string), "Waited 2 seconds.") {
		t.Fatalf("saved: %v", saved)
	}

	// Stopping: the turn ends at once, saying so.
	stopped := make(chan []map[string]any)
	go func() {
		stopped <- readEvents(t, e.do(t, c, "POST", "/api/agent/chat", map[string]any{"chat_id": id, "message": "wait 30"}))
	}()
	time.Sleep(300 * time.Millisecond)
	if code := status(t, e.do(t, c, "POST", "/api/chats/"+id+"/stop", map[string]any{})); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	select {
	case evs := <-stopped:
		if n := len(evs); n < 2 || evs[n-2]["type"] != "stopped" || evs[n-1]["type"] != "done" {
			t.Fatalf("stopped turn: %v", evs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the turn didn't stop")
	}
}

// A turn with no progress for agentIdleTimeout is closed, and says why.
func TestAgentChatIdle(t *testing.T) {
	old := agentIdleTimeout
	agentIdleTimeout = 1200 * time.Millisecond
	defer func() { agentIdleTimeout = old }()
	e := newEnv(t)
	c := e.signIn(t, "idle@example.com")
	// "wait 3" says a word each second: progress, so it isn't closed…
	evs := readEvents(t, e.do(t, c, "POST", "/api/agent/chat", map[string]any{"message": "wait 3"}))
	if last := evs[len(evs)-2]; last["type"] == "error" {
		t.Fatalf("closed while making progress: %v", evs)
	}
	// …but with the timeout under a second, it is.
	agentIdleTimeout = 600 * time.Millisecond
	evs = readEvents(t, e.do(t, c, "POST", "/api/agent/chat", map[string]any{"message": "wait 5"}))
	n := len(evs)
	if n < 2 || evs[n-2]["type"] != "error" || !strings.Contains(evs[n-2]["error"].(string), "no progress") {
		t.Fatalf("idle turn: %v", evs)
	}
}
