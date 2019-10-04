// Copyright 2019 Dean, Inc.
// Authors: Dean
// Date: 2019-10-04 19:53

// 基于Linux netlink Connector捕获进程事件

package psevent

import (
	"bytes"
	"encoding/binary"
	"sync"
	"syscall"
)

type ProcEventFork struct {
	ParentPid int // Pid of the process that called fork()
	ChildPid  int // Child process pid created by fork()
}

type ProcEventExec struct {
	Pid int // Pid of the process that called exec()
}

type ProcEventExit struct {
	Pid int // Pid of the process that called exit()
}

type watch struct {
	flags uint32 // Saved value of Watch() flags param
}

type eventListener interface {
	close() error // Watch.Close() closes the OS specific listener
}

type PsEvent struct {
	listener     eventListener // OS specifics (kqueue or netlink)
	listeners    chan eventListener
	watches      map[int]*watch // Map of watched process ids
	watchesMutex *sync.Mutex

	Error chan error          // Errors are sent on this channel
	Fork  chan *ProcEventFork // Fork events are sent on this channel
	Exec  chan *ProcEventExec // Exec events are sent on this channel
	Exit  chan *ProcEventExit // Exit events are sent on this channel
	done  chan bool           // Used to stop the readEvents() goroutine

	isClosed    bool // Set to true when Close() is first called
	closedMutex *sync.Mutex
}

// Initialize event listener and channels
func Listen() (*PsEvent, error) {
	listener, err := createListener()

	if err != nil {
		return nil, err
	}

	w := &PsEvent{
		listener:    listener,
		Fork:        make(chan *ProcEventFork),
		Exec:        make(chan *ProcEventExec),
		Exit:        make(chan *ProcEventExit),
		Error:       make(chan error),
		done:        make(chan bool, 1),
		closedMutex: &sync.Mutex{},
	}

	go w.readEvents()
	return w, nil
}

// Read events from the netlink socket
func (p *PsEvent) readEvents() {
	buf := make([]byte, syscall.Getpagesize())

	listener, _ := p.listener.(*NetLink)

	for {
		if p.isDone() {
			return
		}

		nr, _, err := syscall.Recvfrom(listener.sock, buf, 0)

		if err != nil {
			p.Error <- err
			continue
		}
		if nr < syscall.NLMSG_HDRLEN {
			p.Error <- syscall.EINVAL
			continue
		}

		msgs, _ := syscall.ParseNetlinkMessage(buf[:nr])

		for _, m := range msgs {
			if m.Header.Type == syscall.NLMSG_DONE {
				p.handleEvent(m.Data)
			}
		}
	}
}

// Close event channels when done message is received
func (p *PsEvent) finish() {
	close(p.Fork)
	close(p.Exec)
	close(p.Exit)
	close(p.Error)
}

// Dispatch events from the netlink socket to the Event channels.
// Unlike bsd kqueue, netlink receives events for all pids,
// so we apply filtering based on the watch table via isWatching()
func (p *PsEvent) handleEvent(data []byte) {
	buf := bytes.NewBuffer(data)
	msg := &cnMsg{}
	hdr := &procEventHeader{}

	_ = binary.Read(buf, byteOrder, msg)
	_ = binary.Read(buf, byteOrder, hdr)

	switch hdr.What {
	case PROC_EVENT_FORK:
		event := &forkProcEvent{}

		_ = binary.Read(buf, byteOrder, event)
		ppid := int(event.ParentTgid)
		pid := int(event.ChildTgid)

		p.Fork <- &ProcEventFork{ParentPid: ppid, ChildPid: pid}

	case PROC_EVENT_EXEC:
		event := &execProcEvent{}
		_ = binary.Read(buf, byteOrder, event)
		pid := int(event.ProcessTgid)
		p.Exec <- &ProcEventExec{Pid: pid}

	case PROC_EVENT_EXIT:
		event := &exitProcEvent{}
		_ = binary.Read(buf, byteOrder, event)
		pid := int(event.ProcessTgid)
		p.Exit <- &ProcEventExit{Pid: pid}
	}
}

// Closes the OS specific event listener,
func (p *PsEvent) Close() error {
	return p.listener.close()
}

// Internal helper to check if there is a message on the "done" channel.
// The "done" message is sent by the Close() method; when received here,
// the Watcher.finish method is called to close all channels and return
// true - in which case the caller should break from the readEvents loop.
func (p *PsEvent) isDone() bool {
	var done bool
	select {
	case done = <-p.done:
		p.finish()
	default:
	}
	return done
}
