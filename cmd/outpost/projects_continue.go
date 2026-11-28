package main

// Keeping a project going. When a task or a research run that the project's conversation started
// finishes, the conversation gets a message from Outpost with what came out of it, and the lead
// answers again: it carries on with the owner's original request (starts the next step), and
// asks only if a real decision is left. Without this the conversation stopped after the first
// task, however much was still to do. At most maxAutoSteps of these in a row without the owner.

import (
	"fmt"
	"strings"
	"time"
)

// maxAutoSteps is how many Outpost-written continuations may follow one message of the owner's.
const maxAutoSteps = 5

const continueAsk = "Carry on with the owner's original request using this. If the next step is clear, start it (a task or research); ask the owner only if a real decision is left, and if everything is done, say so briefly."

// autoSteps counts the continuations since the owner's last message.
func autoSteps(msgs []ChatMsg) int {
	n := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		if msgs[i].Auto == "" {
			break
		}
		n++
	}
	return n
}

// continueProject adds an Outpost message (label is shown, text goes to the model) to the
// project's conversation and answers it. It waits while the conversation is busy.
func (c *Controller) continueProject(chatID, label, text string) {
	ch := c.getChat(chatID)
	if ch == nil || ch.ProjectID == "" || c.getProject(ch.ProjectID) == nil {
		return
	}
	for i := 0; i < 120; i++ {
		ch.mu.Lock()
		if ch.Busy == "" {
			if autoSteps(ch.Messages) >= maxAutoSteps {
				ch.mu.Unlock()
				return
			}
			ch.Messages = append(ch.Messages, ChatMsg{ID: newID("m"), Role: "user", Text: text, Auto: label, At: time.Now()})
			c.beginAnswerLocked(ch, nil, 0)
			return
		}
		ch.mu.Unlock()
		time.Sleep(3 * time.Second)
	}
}

// taskFinishedToChat tells the conversation that started a task how it ended.
func (c *Controller) taskFinishedToChat(t *Task) {
	t.mu.Lock()
	chat, from, round, status, title, id := t.ChatID, t.FromChat, t.Round, t.Status, t.Title, t.ID
	t.mu.Unlock()
	if chat == "" || !from || round != 1 {
		return
	}
	label := "Task finished: " + title
	if status != "done" {
		label = "Task " + status + ": " + title
	}
	c.continueProject(chat, label, taskDigest(t, id, title, status))
}

// taskDigest is what the lead is told about a finished task: how it ended, what an investigation
// found, and the files written.
func taskDigest(t *Task, id, title, status string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "[From Outpost, not the owner] Task %q (%s) %s.", title, id, status)
	if status == "done" {
		b.WriteString(" " + resultText(t))
	} else if t.Error != "" {
		b.WriteString(" " + clip(t.Error, 500))
	}
	b.WriteString("\n")
	var found []string
	for _, st := range t.Subtasks {
		if r := strings.TrimSpace(st.Report); r != "" {
			found = append(found, "Findings from "+st.ID+":\n"+r)
		}
	}
	if len(found) > 0 {
		b.WriteString("\n" + clip(strings.Join(found, "\n\n"), 8000) + "\n")
	}
	if len(t.Touched) > 0 {
		b.WriteString("\nFiles written: " + clip(strings.Join(t.Touched, ", "), 500) + "\n")
	}
	b.WriteString("\n" + continueAsk)
	return b.String()
}

// researchFinishedToChat tells the conversation that a research run is done and where its report is.
func (c *Controller) researchFinishedToChat(chatID, question, saved string) {
	text := fmt.Sprintf("[From Outpost, not the owner] The research on %q finished. Its report is in the conversation above", question)
	if saved != "" {
		text += " and saved in the project's Stash as " + saved
	}
	text += ".\n\n" + continueAsk
	c.continueProject(chatID, "Research finished", text)
}
