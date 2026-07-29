package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type applicationRuntime struct {
	startedAt   time.Time
	config      *configStore
	router      *routerClient
	lightpanda  *lightpandaManager
	tunnel      *tunnelManager
	agents      *agentManager
	adminKey    string
	csrfToken   string
	probeCancel context.CancelFunc
	probeDone   chan struct{}
}

var (
	tautlineRuntimeMu sync.RWMutex
	tautlineRuntime   *applicationRuntime
)

func newApplicationRuntime(store *configStore) (*applicationRuntime, error) {
	lightpanda := newLightpandaManager(store)
	router := newRouterClient(store)
	agents, err := newAgentManager(store, router, lightpanda)
	if err != nil {
		return nil, err
	}
	adminKey, err := loadOrCreateDashboardKey(store.snapshot().RuntimeDir)
	if err != nil {
		return nil, err
	}
	return &applicationRuntime{
		startedAt:  time.Now().UTC(),
		config:     store,
		router:     router,
		lightpanda: lightpanda,
		tunnel:     newTunnelManager(store),
		agents:     agents,
		adminKey:   adminKey,
		csrfToken:  randomHex(24),
		probeDone:  make(chan struct{}),
	}, nil
}

func setApplicationRuntime(runtime *applicationRuntime) {
	tautlineRuntimeMu.Lock()
	tautlineRuntime = runtime
	tautlineRuntimeMu.Unlock()
}

func currentApplicationRuntime() (*applicationRuntime, error) {
	tautlineRuntimeMu.RLock()
	defer tautlineRuntimeMu.RUnlock()
	if tautlineRuntime == nil {
		return nil, fmt.Errorf("Tautline runtime is not initialized")
	}
	return tautlineRuntime, nil
}

func (a *applicationRuntime) startProbes() {
	ctx, cancel := context.WithCancel(context.Background())
	a.probeCancel = cancel
	go func() {
		defer close(a.probeDone)
		go a.lightpanda.probeRunner()
		a.probeRouter(ctx)
		routerTicker := time.NewTicker(30 * time.Second)
		browserTicker := time.NewTicker(60 * time.Second)
		defer routerTicker.Stop()
		defer browserTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-routerTicker.C:
				a.probeRouter(ctx)
			case <-browserTicker.C:
				go a.lightpanda.probeRunner()
			}
		}
	}()
}

func (a *applicationRuntime) probeRouter(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	a.agents.refreshRouterStatus(ctx)
}

func (a *applicationRuntime) shutdown() {
	if a.probeCancel != nil {
		a.probeCancel()
		select {
		case <-a.probeDone:
		case <-time.After(2 * time.Second):
		}
	}
	_ = a.tunnel.stop()
	_ = a.lightpanda.stop()
}

func dashboardKeyPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "state", "dashboard-admin.key")
}

func loadOrCreateDashboardKey(runtimeDir string) (string, error) {
	path := dashboardKeyPath(runtimeDir)
	if data, err := os.ReadFile(path); err == nil {
		key := strings.TrimSpace(string(data))
		if len(key) >= 32 {
			return key, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	key := randomHex(32)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(key+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, path); err != nil {
		return "", err
	}
	return key, nil
}

func randomHex(bytesCount int) string {
	data := make([]byte, bytesCount)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data)
}
