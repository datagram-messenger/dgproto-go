package dgproto

import (
	"math"
	"time"
)

func defaultKeepaliveTimeout(interval time.Duration) (time.Duration, bool) {
	if interval > time.Duration(math.MaxInt64/2) {
		return time.Duration(math.MaxInt64), false
	}
	return 2 * interval, true
}

// beginPing publishes the expectation before enqueue, including for a reply
// arriving before enqueueControl returns. Zero is reserved across rollover.
func (c *Connection) beginPing() uint64 {
	c.pingMu.Lock()
	defer c.pingMu.Unlock()
	if c.ctx.Err() != nil {
		return 0
	}
	nonce := c.pingNonce.Load() + 1
	if nonce == 0 {
		nonce = 1
	}
	c.pingExpected, c.pingAcknowledged = nonce, false
	c.pingNonce.Store(nonce)
	return nonce
}

func (c *Connection) clearPing() {
	c.pingMu.Lock()
	c.pingExpected, c.pingAcknowledged = 0, false
	c.pingMu.Unlock()
}

func (c *Connection) acknowledgePing(nonce uint64) {
	c.pingMu.Lock()
	defer c.pingMu.Unlock()
	if c.pingExpected == 0 || nonce != c.pingExpected || c.ctx.Err() != nil {
		return
	}
	c.pingExpected, c.pingAcknowledged = 0, true
	select {
	case c.pong <- nonce:
	default:
	}
}

func (c *Connection) consumePingAcknowledgement() bool {
	c.pingMu.Lock()
	defer c.pingMu.Unlock()
	acknowledged := c.pingAcknowledged
	c.pingAcknowledged = false
	return acknowledged
}

func (c *Connection) maintenanceLoop() {
	var idleTimer, keepaliveTimer, pongTimer *time.Timer
	var idle, keepalive, pongDeadline <-chan time.Time
	if c.config.IdleTimeout > 0 {
		idleTimer = time.NewTimer(c.config.IdleTimeout)
		idle = idleTimer.C
		defer idleTimer.Stop()
	}
	if c.config.KeepaliveInterval > 0 {
		keepaliveTimer = time.NewTimer(c.config.KeepaliveInterval)
		keepalive = keepaliveTimer.C
		defer keepaliveTimer.Stop()
	}
	var outstanding uint64

	for {
		select {
		case <-c.ctx.Done():
			if pongTimer != nil {
				pongTimer.Stop()
			}
			return
		case <-c.activity:
			resetTimer(idleTimer, c.config.IdleTimeout)
		case <-c.pong:
			if outstanding != 0 && c.consumePingAcknowledgement() {
				outstanding = 0
				if pongTimer != nil {
					if !pongTimer.Stop() {
						select {
						case <-pongTimer.C:
						default:
						}
					}
					pongDeadline = nil
				}
				resetTimer(keepaliveTimer, c.config.KeepaliveInterval)
				keepalive = keepaliveTimer.C
			}
		case <-keepalive:
			if outstanding != 0 {
				continue
			}
			outstanding = c.beginPing()
			if outstanding == 0 {
				return
			}
			if err := c.enqueueControl(PingPong{Nonce: outstanding}, false); err != nil {
				c.clearPing()
				outstanding = 0
				resetTimer(keepaliveTimer, c.config.KeepaliveInterval)
				continue
			}
			pongTimer = time.NewTimer(c.config.KeepaliveTimeout)
			pongDeadline = pongTimer.C
			keepalive = nil
		case <-pongDeadline:
			// A ready timer must not beat an acknowledgement already recorded
			// by the reader just because select chose the timer wakeup first.
			if c.consumePingAcknowledgement() {
				outstanding = 0
				pongDeadline = nil
				resetTimer(keepaliveTimer, c.config.KeepaliveInterval)
				keepalive = keepaliveTimer.C
				continue
			}
			c.shutdown(ErrKeepaliveTimeout)
			return
		case <-idle:
			_ = c.closeGracefully(SessionClose{Code: 3, Reason: "idle timeout"}, ErrIdleTimeout)
			return
		}
	}
}

func (c *Connection) noteActivity() {
	select {
	case c.activity <- struct{}{}:
	default:
	}
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}
