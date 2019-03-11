// queue.go - Client egress queue.
// Copyright (C) 2018  masala, David Stainton.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package session

import (
	"fmt"
	"sync"
	"time"

	"github.com/katzenpost/core/queue"
	"github.com/katzenpost/core/worker"
)

type nqueue interface {
	Push(*Message) error
}

// TimerQ is a queue that delays messages before forwarding to another queue
type TimerQ struct {
	sync.Mutex
	sync.Cond
	worker.Worker

	priq   *queue.PriorityQueue
	nextQ  nqueue

	timer  *time.Timer
	wakech chan struct{}
}

// NewTimerQ intantiates a new TimerQ and starts the worker routine
func NewTimerQ(q nqueue) *TimerQ {
	a := &TimerQ{
		nextQ: q,
		timer: time.NewTimer(0),
		priq:  queue.New(),
	}
	a.L = new(sync.Mutex)
	a.Go(a.worker)
	return a
}

// Push adds a message to the TimerQ
func (a *TimerQ) Push(m *Message) {
	a.Lock()
	a.priq.Enqueue(m.expiry(), m)
	a.Unlock()
	a.Signal()
}

// Remove removes a Message from the TimerQ
func (a *TimerQ) Remove(m *Message) error {
	a.Lock()
	defer a.Unlock()
	if mo := a.priq.Peek(); mo != nil {
		if mo.Value.(*Message) == m {
			a.priq.Pop()
			if a.priq.Len() > 0 {
				// wake up the worker to reset the timer
				a.Signal()
			}
		}
	} else {
		mo := a.priq.Remove(m.expiry())
		switch mo {
		case m == mo.(*Message):
		case nil:
		default:
			return fmt.Errorf("Failed to remove %v", m)
			defer a.Push(mo.(*Message))
		}
	}
	return nil
}

// wakeupCh() returns the channel that fires upon Signal of the TimerQ's sync.Cond
func (a *TimerQ) wakeupCh() chan struct{} {
	if a.wakech != nil {
		return a.wakech
	}
	c := make(chan struct{})
	go func() {
		defer close(c)
		var v struct{}
		for {
			a.L.Lock()
			a.Wait()
			a.L.Unlock()
			select {
			case <-a.HaltCh():
				return
			case c <- v:
			}
		}
	}()
	a.wakech = c
	return c
}

// pop top item from queue and forward to next queue
func (a *TimerQ) forward() {
	a.Lock()
	m := a.priq.Pop()
	a.Unlock()
	if m == nil {
		return
	}


	if err := a.nextQ.Push(m.Value.(*Message)); err != nil {
		panic(err)
	}
}

func (a *TimerQ) worker() {
	for {
		var c <-chan time.Time
		a.Lock()
		if m := a.priq.Peek(); m != nil {
			msg := m.Value.(*Message)
			tl := msg.timeLeft()
			if tl < 0 {
				a.Unlock()
				a.forward()
				continue
			} else {
				c = time.After(tl)
			}
		}
		a.Unlock()
		select {
		case <-a.HaltCh():
			return
		case <-c:
			a.forward()
		case <-a.wakeupCh():
		}
	}
}
