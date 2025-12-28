package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddr   string
	DBPath       string
	AuthToken    string
	MaxBodyBytes int64
	WriteQueue   int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

func Default() Config {
	return Config{
		ListenAddr:   "127.0.0.1:7777",
		DBPath:       "waitmesh.db",
		MaxBodyBytes: 1 << 20,
		WriteQueue:   1024,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

func FromEnv() Config {
	c := Default()
	if v := os.Getenv("WAITMESH_LISTEN"); v != "" {
		c.ListenAddr = v
	}
	if v := os.Getenv("WAITMESH_DB"); v != "" {
		c.DBPath = v
	}
	if v := os.Getenv("WAITMESH_TOKEN"); v != "" {
		c.AuthToken = v
	}
	if v := os.Getenv("WAITMESH_MAX_BODY"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			c.MaxBodyBytes = n
		}
	}
	return c
}

func (c Config) Validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("listen address required")
	}
	if c.MaxBodyBytes <= 0 {
		return fmt.Errorf("max body bytes must be positive")
	}
	if IsNonLoopbackListen(c.ListenAddr) && c.AuthToken == "" {
		return fmt.Errorf("WAITMESH_TOKEN required when binding non-loopback address")
	}
	return nil
}

// IsNonLoopbackListen reports whether addr would accept remote connections.
func IsNonLoopbackListen(addr string) bool {
	host := addr
	if i := len(addr) - 1; i >= 0 {
		// strip :port for host checks; handle ":7777" and "host:port"
		if idx := lastColonHost(addr); idx >= 0 {
			host = addr[:idx]
		}
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return true
	}
	if hasPrefix(host, "127.0.0.1") || host == "localhost" || host == "[::1]" || host == "::1" {
		return false
	}
	return true
}

func lastColonHost(addr string) int {
	// Avoid splitting IPv6 without brackets incorrectly: require form host:port
	if hasPrefix(addr, "[") {
		if end := indexByte(addr, ']'); end >= 0 && end+1 < len(addr) && addr[end+1] == ':' {
			return end + 1
		}
		return -1
	}
	return lastIndexByte(addr, ':')
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}
