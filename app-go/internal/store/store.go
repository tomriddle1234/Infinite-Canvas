package store

import (
	"sync"

	"infinite-canvas/app-go/internal/config"
)

type Store struct {
	cfg            *config.Config
	canvasMu       sync.Mutex
	conversationMu sync.Mutex
	providerMu     sync.Mutex
	envMu          sync.Mutex
	seedanceMu     sync.Mutex
}

func New(cfg *config.Config) *Store {
	return &Store{cfg: cfg}
}
