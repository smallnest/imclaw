package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Config represents the main configuration
type Config struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Timeout   int    `json:"timeout"`
	AuthToken string `json:"auth_token"`
}

// Manager manages configuration
type Manager struct {
	mu         sync.RWMutex
	config     *Config
	configPath string
	watcher    *fsnotify.Watcher
	onChange   func(*Config)
	stopCh     chan struct{}
}

// NewManager creates a new config manager
func NewManager(configPath string) (*Manager, error) {
	mgr := &Manager{
		configPath: configPath,
		stopCh:     make(chan struct{}),
	}

	if err := mgr.load(); err != nil {
		return nil, err
	}

	return mgr, nil
}

// load loads the configuration from file
func (m *Manager) load() error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	m.setDefaults(&cfg)

	m.mu.Lock()
	m.config = &cfg
	m.mu.Unlock()

	return nil
}

// setDefaults sets default values for configuration
func (m *Manager) setDefaults(cfg *Config) {
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30
	}
}

// Get returns the current configuration
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// SetOnChange sets the callback for configuration changes
func (m *Manager) SetOnChange(callback func(*Config)) {
	m.onChange = callback
}

// Watch starts watching for configuration file changes
func (m *Manager) Watch() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Failed to create config watcher: %v", err)
		return
	}

	m.watcher = watcher

	if err := watcher.Add(m.configPath); err != nil {
		log.Printf("Failed to watch config file: %v", err)
		return
	}

	go func() {
		var debounceTimer *time.Timer
		for {
			select {
			case <-m.stopCh:
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					// Debounce rapid writes
					if debounceTimer != nil {
						debounceTimer.Stop()
					}
					debounceTimer = time.AfterFunc(500*time.Millisecond, func() {
						log.Printf("Config file changed, reloading...")
						if err := m.load(); err != nil {
							log.Printf("Failed to reload config: %v", err)
						} else if m.onChange != nil {
							m.onChange(m.Get())
						}
					})
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Config watcher error: %v", err)
			}
		}
	}()
}

// Stop stops the config manager
func (m *Manager) Stop() {
	close(m.stopCh)
	if m.watcher != nil {
		m.watcher.Close()
	}
}

// GetDefaultConfigPath returns the default config file path
func GetDefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".imclaw", "config.json"), nil
}
