package agenthub

import (
	"encoding/json"
	"sync"
)

type streamSession struct {
	mu       sync.Mutex
	messages chan json.RawMessage
	done     chan struct{}
	stopped  bool
}

func newStreamSession() *streamSession {
	return &streamSession{
		messages: make(chan json.RawMessage, 16),
		done:     make(chan struct{}),
	}
}

func (s *streamSession) publish(message json.RawMessage) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return false
	}
	message = append(json.RawMessage(nil), message...)
	select {
	case s.messages <- message:
		return true
	default:
		return false
	}
}

func (s *streamSession) stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.done)
}

func (c *agentConnection) startLogTailSession(sessionID string, send func() error) (*streamSession, error) {
	session := newStreamSession()
	c.registerLogTail(sessionID, session)
	if err := send(); err != nil {
		c.unregisterLogTail(sessionID, session)
		return nil, err
	}
	return session, nil
}

func (c *agentConnection) registerLogTail(sessionID string, session *streamSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.logTailSessions == nil {
		c.logTailSessions = make(map[string]*streamSession)
	}
	c.logTailSessions[sessionID] = session
}

func (c *agentConnection) unregisterLogTail(sessionID string, session *streamSession) {
	c.mu.Lock()
	if c.logTailSessions[sessionID] == session {
		delete(c.logTailSessions, sessionID)
	}
	c.mu.Unlock()
	session.stop()
}

func (c *agentConnection) publishLogTail(sessionID string, message json.RawMessage) bool {
	c.mu.Lock()
	session := c.logTailSessions[sessionID]
	c.mu.Unlock()
	return session.publish(message)
}

func (c *agentConnection) startContainerLogsSession(sessionID string, send func() error) (*streamSession, error) {
	session := newStreamSession()
	c.registerContainerLogs(sessionID, session)
	if err := send(); err != nil {
		c.unregisterContainerLogs(sessionID, session)
		return nil, err
	}
	return session, nil
}

func (c *agentConnection) registerContainerLogs(sessionID string, session *streamSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.containerLogsSessions == nil {
		c.containerLogsSessions = make(map[string]*streamSession)
	}
	c.containerLogsSessions[sessionID] = session
}

func (c *agentConnection) unregisterContainerLogs(sessionID string, session *streamSession) {
	c.mu.Lock()
	if c.containerLogsSessions[sessionID] == session {
		delete(c.containerLogsSessions, sessionID)
	}
	c.mu.Unlock()
	session.stop()
}

func (c *agentConnection) publishContainerLogs(sessionID string, message json.RawMessage) bool {
	c.mu.Lock()
	session := c.containerLogsSessions[sessionID]
	c.mu.Unlock()
	return session.publish(message)
}

func (c *agentConnection) closeStreamSessions() {
	c.mu.Lock()
	sessions := make([]*streamSession, 0, len(c.logTailSessions)+len(c.containerLogsSessions))
	for sessionID, session := range c.logTailSessions {
		sessions = append(sessions, session)
		delete(c.logTailSessions, sessionID)
	}
	for sessionID, session := range c.containerLogsSessions {
		sessions = append(sessions, session)
		delete(c.containerLogsSessions, sessionID)
	}
	c.mu.Unlock()
	for _, session := range sessions {
		session.stop()
	}
}
