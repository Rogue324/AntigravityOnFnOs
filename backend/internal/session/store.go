package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"time"
)

var ErrNotFound = errors.New("session not found")

type ToolCall struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
	Status string `json:"status"` // running | done | error
}

type TokenUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

type Message struct {
	ID        string      `json:"id"`
	Role      string      `json:"role"` // user | assistant
	Content   string      `json:"content"`
	Thinking  string      `json:"thinking,omitempty"`
	ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
	Usage     *TokenUsage `json:"usage,omitempty"`
	Error     string      `json:"error,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

type Session struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Workspace      string    `json:"workspace"`
	ExtraDirs      []string  `json:"extra_dirs,omitempty"`
	Model          string    `json:"model,omitempty"`
	Effort         string    `json:"effort,omitempty"`
	PermissionMode string    `json:"permission_mode,omitempty"`
	Messages       []Message `json:"messages"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Summary is a lightweight projection for sidebar listing.
type Summary struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Workspace      string    `json:"workspace"`
	ExtraDirs      []string  `json:"extra_dirs,omitempty"`
	Model          string    `json:"model,omitempty"`
	Effort         string    `json:"effort,omitempty"`
	PermissionMode string    `json:"permission_mode,omitempty"`
	MessageCount   int       `json:"message_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Store struct {
	mu       sync.RWMutex
	filePath string
	sessions map[string]*Session
}

func NewStore(filePath string) (*Store, error) {
	s := &Store{
		filePath: filePath,
		sessions: make(map[string]*Session),
	}
	if err := s.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

func (s *Store) List() []Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]Summary, 0, len(s.sessions))
	for _, sess := range s.sessions {
		list = append(list, Summary{
			ID:             sess.ID,
			Title:          sess.Title,
			ConversationID: sess.ConversationID,
			Workspace:      sess.Workspace,
			ExtraDirs:      append([]string(nil), sess.ExtraDirs...),
			Model:          sess.Model,
			Effort:         sess.Effort,
			PermissionMode: sess.PermissionMode,
			MessageCount:   len(sess.Messages),
			CreatedAt:      sess.CreatedAt,
			UpdatedAt:      sess.UpdatedAt,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})
	return list
}

func (s *Store) Get(id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneSession(sess), nil
}

func (s *Store) Create(title, workspace string, extraDirs []string, model, effort, permMode string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if title == "" {
		title = "新对话"
	}
	sess := &Session{
		ID:             newID(),
		Title:          title,
		Workspace:      workspace,
		ExtraDirs:      append([]string(nil), extraDirs...),
		Model:          model,
		Effort:         effort,
		PermissionMode: permMode,
		Messages:       []Message{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	s.sessions[sess.ID] = sess
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return cloneSession(sess), nil
}

func (s *Store) Update(id string, fn func(*Session)) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	fn(sess)
	sess.UpdatedAt = time.Now()
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return cloneSession(sess), nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return ErrNotFound
	}
	delete(s.sessions, id)
	return s.saveLocked()
}

func (s *Store) AppendMessage(id string, msg Message) (*Session, error) {
	if msg.ID == "" {
		msg.ID = newID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	return s.Update(id, func(sess *Session) {
		sess.Messages = append(sess.Messages, msg)
	})
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	var list []*Session
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	for _, sess := range list {
		if sess.ID != "" {
			s.sessions[sess.ID] = sess
		}
	}
	return nil
}

func (s *Store) saveLocked() error {
	list := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		list = append(list, sess)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath)
}

func cloneSession(src *Session) *Session {
	raw, _ := json.Marshal(src)
	var dst Session
	_ = json.Unmarshal(raw, &dst)
	return &dst
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
