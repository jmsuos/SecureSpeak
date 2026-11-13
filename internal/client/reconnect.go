package client

// Automatic reconnecting. When the connection is lost without the user
// disconnecting (internet trouble, the server stopping or restarting for an
// update), the client keeps trying the same server with growing pauses and
// puts the user back in the channel they were in. Kicks, bans, a changed
// server certificate or a wrong password stop it.

import (
	"errors"
	"math/rand/v2"
	"time"

	"securespeak/internal/proto"
)

// ServerError is a refusal sent by the server while connecting.
type ServerError struct{ Code, Msg string }

func (e *ServerError) Error() string { return e.Msg }

type connParams struct {
	host     string
	port     int
	nick, pw string
}

type reconnector struct {
	params   connParams
	label    string // server name for messages
	channel  uint32 // channel to return to
	chanName string
	attempt  int
	next     time.Time
	cancel   chan struct{}
	now      chan struct{}
}

// uiReconnect is shown while reconnecting.
type uiReconnect struct {
	Server  string `json:"server"`
	Attempt int    `json:"attempt"`
	NextAt  int64  `json:"nextAt"` // unix ms of the next attempt
}

var reconnectDelays = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second, 60 * time.Second}

// permanentConnectError reports errors that retrying will not fix.
func permanentConnectError(err error) bool {
	var ue *UntrustedError
	if errors.As(err, &ue) {
		return true
	}
	var se *ServerError
	if errors.As(err, &se) {
		return se.Code != "full" // full: someone may leave
	}
	return false
}

// startReconnectLocked begins reconnecting after an unexpected drop.
func (c *Client) startReconnectLocked(label string, channel uint32, chanName string) {
	if c.rc != nil || c.closing || c.cfg.NoAutoReconnect || c.lastConn.host == "" {
		return
	}
	rc := &reconnector{params: c.lastConn, label: label, channel: channel, chanName: chanName,
		cancel: make(chan struct{}), now: make(chan struct{}, 1)}
	c.rc = rc
	go c.reconnectLoop(rc)
}

func (c *Client) reconnectLoop(rc *reconnector) {
	defer func() {
		c.mu.Lock()
		if c.rc == rc {
			c.rc = nil
		}
		c.mu.Unlock()
		c.emitState()
	}()
	for {
		d := reconnectDelays[min(rc.attempt, len(reconnectDelays)-1)]
		d += time.Duration(rand.Int64N(int64(d) / 5)) // jitter, so a restarted server isn't hit by everyone at once
		c.mu.Lock()
		rc.attempt++
		rc.next = time.Now().Add(d)
		c.mu.Unlock()
		c.emitState()
		t := time.NewTimer(d)
		select {
		case <-t.C:
		case <-rc.now:
			t.Stop()
		case <-rc.cancel:
			t.Stop()
			return
		case <-c.closed:
			t.Stop()
			return
		}
		select {
		case <-rc.cancel:
			return
		default:
		}
		p := rc.params
		err := c.Connect(p.host, p.port, p.nick, p.pw, "")
		if err == nil {
			c.rejoin(rc)
			return
		}
		c.mu.Lock()
		connected := c.sess != nil
		c.mu.Unlock()
		if connected { // the user connected somewhere else meanwhile
			return
		}
		if permanentConnectError(err) {
			var ue *UntrustedError
			if errors.As(err, &ue) {
				c.system("⚠ Stopped reconnecting: the server's certificate has changed. Connect manually to check the new fingerprint.")
			} else {
				c.system("Stopped reconnecting: " + err.Error())
			}
			return
		}
	}
}

// rejoin returns to the channel we were in, if it still exists.
func (c *Client) rejoin(rc *reconnector) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.sess
	if s == nil || rc.channel == 0 || s.me.Channel == rc.channel {
		return
	}
	ch, ok := s.channels[rc.channel]
	if !ok || ch.Name != rc.chanName {
		return
	}
	s.send(proto.TJoin, proto.Join{Channel: ch.ID, Password: c.chanPw[ch.ID]})
}

// cancelReconnect stops a running reconnect (manual connect/disconnect).
func (c *Client) cancelReconnectLocked() {
	if c.rc != nil {
		close(c.rc.cancel)
		c.rc = nil
	}
}

func (c *Client) reconnectStateLocked() *uiReconnect {
	if c.rc == nil {
		return nil
	}
	return &uiReconnect{Server: c.rc.label, Attempt: c.rc.attempt, NextAt: c.rc.next.UnixMilli()}
}
