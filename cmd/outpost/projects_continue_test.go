package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAutoStepsCount(t *testing.T) {
	msgs := []ChatMsg{{Role: "user", Text: "a"}, {Role: "assistant"}, {Role: "user", Auto: "x"}, {Role: "assistant"}, {Role: "user", Auto: "y"}}
	if n := autoSteps(msgs); n != 2 {
		t.Fatalf("got %d", n)
	}
	if n := autoSteps(append(msgs, ChatMsg{Role: "user", Text: "real"})); n != 0 {
		t.Fatalf("owner message resets: %d", n)
	}
}

func TestResultTextInvestigationHasNoTestNote(t *testing.T) {
	tk := &Task{Status: "done", TestStatus: "skipped", Subtasks: make([]*SubtaskState, 3)}
	if s := resultText(tk); strings.Contains(s, "automated tests") || !strings.HasPrefix(s, "Done.") {
		t.Fatalf("investigation: %q", s)
	}
	tk.Touched = []string{"a.go"}
	if s := resultText(tk); !strings.Contains(s, "no automated tests") {
		t.Fatalf("code task: %q", s)
	}
}

func TestTaskDigestCarriesFindings(t *testing.T) {
	tk := &Task{Status: "done", TestStatus: "skipped", Subtasks: []*SubtaskState{{ID: "s1", Report: "Hermes is the Nous model family."}}}
	d := taskDigest(tk, "task_1", "Hermes research", "done")
	for _, want := range []string{"From Outpost", "Hermes is the Nous model family.", "original request"} {
		if !strings.Contains(d, want) {
			t.Fatalf("missing %q in %s", want, d)
		}
	}
}

func TestContinueProjectAnswersAndCaps(t *testing.T) {
	c, p, _, _ := projectWebRig(t, true, fmt.Sprintf(claudeText, "Next step noted."))
	ch := c.projectConvChat(p, true)
	ch.mu.Lock()
	ch.Messages = append(ch.Messages, ChatMsg{ID: "u1", Role: "user", Text: "build a replacement", At: time.Now()})
	for i := 0; i < maxAutoSteps; i++ {
		ch.Messages = append(ch.Messages, ChatMsg{ID: newID("m"), Role: "assistant", Text: "ok"}, ChatMsg{ID: newID("m"), Role: "user", Auto: "x", Text: "x"})
	}
	n := len(ch.Messages)
	ch.mu.Unlock()
	c.continueProject(ch.ID, "Task finished", "text")
	ch.mu.Lock()
	if len(ch.Messages) != n {
		t.Fatalf("a sixth continuation was added")
	}
	ch.Messages = ch.Messages[:2] // owner message + one reply
	ch.mu.Unlock()
	c.continueProject(ch.ID, "Task finished", "[From Outpost] done")
	ch.mu.Lock()
	last := ch.Messages[len(ch.Messages)-1]
	ch.mu.Unlock()
	if last.Role != "user" || last.Auto != "Task finished" {
		t.Fatalf("last %+v", last)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ch.mu.Lock()
		done := ch.Busy == "" && len(ch.Messages) > 3
		ch.mu.Unlock()
		if done {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the lead did not answer the continuation")
}
