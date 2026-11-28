package main

// Limits that always apply to unattended runs (a schedule, or a headless saved
// task started by hand, by a webhook or by a "Run saved tasks" token): nobody
// is watching, so commands only run in a sandbox, and a run cannot go on forever.

import "time"

// unattendedMaxRun is the longest an unattended task may stay active; after
// that it is stopped, the files written so far are kept and the run is noted.
const unattendedMaxRun = 4 * time.Hour

// execNodesFor is execNodes for work done on behalf of t. For an unattended
// task, nodes whose sandbox is off are left out, whatever the global setting
// says: a command written by a model and run with nobody watching must be
// contained. With no sandboxed node the tests and build are skipped and the
// task log says so, the same as with no exec node at all.
func (c *Controller) execNodesFor(t *Task) []string {
	all := c.execNodes()
	if t == nil {
		return all
	}
	t.mu.Lock()
	unattended := t.Unattended
	t.mu.Unlock()
	if !unattended {
		return all
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, id := range all {
		n := c.nodes[id]
		if n != nil && effectiveSandbox(c.settings.Sandbox, n.Sandbox) != "off" {
			out = append(out, id)
		}
	}
	return out
}

// stopOverlongUnattended stops unattended tasks that have been active for more
// than unattendedMaxRun. It runs from the schedule loop.
func (c *Controller) stopOverlongUnattended(now time.Time) {
	c.mu.Lock()
	var list []*Task
	for _, t := range c.tasks {
		list = append(list, t)
	}
	c.mu.Unlock()
	for _, t := range list {
		t.mu.Lock()
		over := t.Unattended && now.Sub(t.Created) > unattendedMaxRun
		cancel := t.cancel
		t.mu.Unlock()
		if !over || !taskActive(t) {
			continue
		}
		if cancel != nil {
			cancel()
		}
		t.setStatus("failed")
		t.logf("warn", "Stopped: an unattended task may run for at most %s. Files written so far are kept.", unattendedMaxRun)
	}
}
