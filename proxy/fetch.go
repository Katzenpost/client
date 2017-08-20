// retrieve_messages.go - client message retrieval
// Copyright (C) 2017  David Stainton.
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

// Package proxy provides mixnet client proxies
package proxy

import (
	"time"

	"github.com/katzenpost/client/scheduler"
	"github.com/katzenpost/client/session_pool"
	"github.com/katzenpost/core/wire/commands"
)

type Fetcher struct {
	Identity string
	sequence uint32
	pool     *session_pool.SessionPool
}

func (f *Fetcher) Fetch() (uint8, error) {
	var queueHintSize uint8
	session, mutex, err := f.pool.Get(account)
	if err != nil {
		return err
	}
	mutex.Lock()
	defer mutex.Unlock()
	cmd := commands.RetrieveMessage{
		Sequence: f.sequence,
	}
	err = session.SendCommand(cmd)
	if err != nil {
		return uint8(0), err
	}
	recvCmd, err := session.RecvCommand()
	if err != nil {
		return uint8(0), err
	}
	if ack, ok := recvCmd.(commands.MessageACK); ok {
		log.Debug("retrieved MessageACK")
		queueHintSize = ack.QueueSizeHint
		err := f.processAck(ack)
		if err != nil {
			return uint8(0), err
		}
	} else if message, ok := recvCmd.(commands.Message); ok {
		log.Debug("retrieved Message")
		queueHintSize = message.QueueSizeHint
		err := f.processMessage(message)
		if err != nil {
			return uint8(0), err
		}
	} else {
		err := errors.New("retrieved non-Message/MessageACK wire protocol command")
		log.Debug(err)
		return uint8(0), err
	}
	r.sequences[account] += 1
	return queueHintSize, nil
}

func (f *Fetcher) processAck(ack *commands.MesageACK) error {

	return nil
}

func (f *Fetcher) processMessage(message *commands.Message) error {

	return nil
}

type FetchScheduler struct {
	fetchers []Fetcher
	sched    *scheduler.PriorityScheduler
	duration time.Duration
}

// NewFetchScheduler creates a new FetchScheduler given a slice of identity strings
// and a duration
func NewFetchScheduler(fetchers []Fetcher, duration time.Duration) *MessageRetriever {
	r := MessageRetriever{
		fetchers: fetchers,
		duration: duration,
	}
	r.sched = scheduler.New(r.handleTask)
	return &r
}

// Start starts our periodic message checking scheduler
func (r *FetchScheduler) Start() {
	for _, fetcher := range r.fetchers {
		r.sched.Add(r.duration, fetcher.Identity)
	}
}

// handleTask recieves a task from the priority queue backed scheduler
// and asserts it's a string before passing it to our message retrieval method
func (r *FetchScheduler) handleTask(task interface{}) {
	identity, ok := task.(string)
	if !ok {
		log.Error("MessageRetriever got invalid task from priority scheduler.")
		return
	}
	queueSizeHint, err := fetchers[identity].Fetch()
	if err != nil {
		log.Error(err)
		return
	}
	if queueSizeHint == 0 {
		r.sched.Add(r.duration, identity)
	} else {
		r.sched.Add(time.Duration(0), identity)
	}
	return
}