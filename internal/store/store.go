// Package store — хранилище состояния в JSON-файле (data/state.json).
// Помнит спарсенные посты и очередь картинок, чтобы не было дублей.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	StatusQueued  = "queued"
	StatusPosted  = "posted"
	StatusFailed  = "failed"
	StatusSkipped = "skipped" // дубликат по хешу картинки
)

type Image struct {
	ID       int    `json:"id"`
	URL      string `json:"url"`
	PostURL  string `json:"post_url"`
	Status   string `json:"status"`
	Hash     string `json:"hash,omitempty"` // sha256 содержимого картинки
	AddedAt  int64  `json:"added_at"`
	PostedAt int64  `json:"posted_at,omitempty"`
	Error    string `json:"error,omitempty"`
}

type State struct {
	Posts        map[string]int64 `json:"posts"`         // url -> added_at
	Images       []Image          `json:"images"`
	PostedHashes map[string]bool  `json:"posted_hashes"` // sha256 уже опубликованных картинок
	NextID       int              `json:"next_id"`

	path   string
	urlSet map[string]bool // индекс URL картинок для быстрой проверки дублей
}

type Stats struct {
	PostsSeen, Queued, Posted, Failed, Skipped int
}

func now() int64 { return time.Now().Unix() }

// Load читает состояние из файла (или создаёт пустое).
func Load(path string) (*State, error) {
	st := &State{
		Posts:        map[string]int64{},
		PostedHashes: map[string]bool{},
		NextID:       1,
		path:         path,
		urlSet:       map[string]bool{},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, err
	}
	if st.Posts == nil {
		st.Posts = map[string]int64{}
	}
	if st.PostedHashes == nil {
		st.PostedHashes = map[string]bool{}
	}
	st.path = path
	st.urlSet = make(map[string]bool, len(st.Images))
	for _, im := range st.Images {
		st.urlSet[im.URL] = true
	}
	if st.NextID == 0 {
		st.NextID = 1
	}
	return st, nil
}

// Save атомарно записывает состояние на диск.
func (s *State) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *State) PostSeen(url string) bool {
	_, ok := s.Posts[url]
	return ok
}

func (s *State) MarkPostSeen(url string) {
	s.Posts[url] = now()
}

// AddImage добавляет картинку в очередь. Возвращает true, если добавлена (не дубль).
func (s *State) AddImage(url, postURL string) bool {
	if s.urlSet[url] {
		return false
	}
	s.urlSet[url] = true
	s.Images = append(s.Images, Image{
		ID:      s.NextID,
		URL:     url,
		PostURL: postURL,
		Status:  StatusQueued,
		AddedAt: now(),
	})
	s.NextID++
	return true
}

func (s *State) QueuedCount() int {
	n := 0
	for _, im := range s.Images {
		if im.Status == StatusQueued {
			n++
		}
	}
	return n
}

// NextQueued возвращает до limit картинок в статусе queued (по возрастанию ID).
func (s *State) NextQueued(limit int) []Image {
	var q []Image
	for _, im := range s.Images {
		if im.Status == StatusQueued {
			q = append(q, im)
		}
	}
	sort.Slice(q, func(i, j int) bool { return q[i].ID < q[j].ID })
	if limit > 0 && len(q) > limit {
		q = q[:limit]
	}
	return q
}

// HashSeen сообщает, публиковалась ли уже картинка с таким содержимым.
func (s *State) HashSeen(hash string) bool {
	return hash != "" && s.PostedHashes[hash]
}

func (s *State) MarkPosted(id int, hash string) {
	if hash != "" {
		s.PostedHashes[hash] = true
	}
	for i := range s.Images {
		if s.Images[i].ID == id {
			s.Images[i].Status = StatusPosted
			s.Images[i].Hash = hash
			s.Images[i].PostedAt = now()
			s.Images[i].Error = ""
			return
		}
	}
}

// MarkSkipped помечает картинку как дубликат по хешу (в канал не пойдёт).
func (s *State) MarkSkipped(id int, hash, reason string) {
	for i := range s.Images {
		if s.Images[i].ID == id {
			s.Images[i].Status = StatusSkipped
			s.Images[i].Hash = hash
			s.Images[i].Error = reason
			return
		}
	}
}

func (s *State) MarkFailed(id int, errMsg string) {
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	for i := range s.Images {
		if s.Images[i].ID == id {
			s.Images[i].Status = StatusFailed
			s.Images[i].Error = errMsg
			return
		}
	}
}

func (s *State) Stats() Stats {
	st := Stats{PostsSeen: len(s.Posts)}
	for _, im := range s.Images {
		switch im.Status {
		case StatusQueued:
			st.Queued++
		case StatusPosted:
			st.Posted++
		case StatusFailed:
			st.Failed++
		case StatusSkipped:
			st.Skipped++
		}
	}
	return st
}
