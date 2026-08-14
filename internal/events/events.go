// Package events turns low-level filesystem events into a scan trigger.
// Events are intentionally coarse: any event under a watched root marks
// the tree dirty and the next periodic scan runs early. One watch per
// root keeps the watcher cheap and the code simple.
package events

import "github.com/fsnotify/fsnotify"

// Trigger delivers a signal whenever something changes under the roots.
// Bursts of events coalesce into at most one pending signal.
type Trigger struct {
	w    *fsnotify.Watcher
	ch   chan struct{}
	done chan struct{}
}

// New watches paths and returns a trigger for them.
func New(paths []string) (*Trigger, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		if err := w.Add(p); err != nil {
			w.Close()
			return nil, err
		}
	}
	t := &Trigger{w: w, ch: make(chan struct{}, 1), done: make(chan struct{})}
	go t.loop()
	return t, nil
}

func (t *Trigger) loop() {
	defer close(t.done)
	for {
		select {
		case _, ok := <-t.w.Events:
			if !ok {
				return
			}
			select {
			case t.ch <- struct{}{}:
			default: // a signal is already pending
			}
		case _, ok := <-t.w.Errors:
			if !ok {
				return
			}
		}
	}
}

// C returns the signal channel: a value means "something changed".
func (t *Trigger) C() <-chan struct{} { return t.ch }

// Close stops the watcher.
func (t *Trigger) Close() error {
	err := t.w.Close()
	<-t.done
	return err
}
