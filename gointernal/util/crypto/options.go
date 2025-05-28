package crypto

import (
	"context"
	"os"

	"github.com/whatap/golib/config"
	"github.com/whatap/golib/logger"
)

type cryptoConfig struct {
	Log            logger.Logger
	ctx            context.Context
	cancel         context.CancelFunc
	Config         config.Config
	ConfigObserver *config.ConfigObserver

	CypherLevel  int32
	EncryptLevel int32
	WhatapHome   string
}

type CryptoOption interface {
	apply(*cryptoConfig)
}
type funcCryptoOption struct {
	f func(*cryptoConfig)
}

func (this *funcCryptoOption) apply(c *cryptoConfig) {
	this.f(c)
}

const (
	defaultNetTimeout = 60
)

var (
	// default
	conf = &cryptoConfig{
		Log:          &logger.EmptyLogger{},
		CypherLevel:  128,
		EncryptLevel: 2,
		WhatapHome:   os.Getenv("WHATAP_HOME"),
	}
)

func newFuncCryptoOption(f func(*cryptoConfig)) *funcCryptoOption {
	return &funcCryptoOption{
		f: f,
	}
}

func WithContext(ctx context.Context, cancel context.CancelFunc) CryptoOption {
	return newFuncCryptoOption(func(c *cryptoConfig) {
		c.ctx = ctx
		c.cancel = cancel
	})
}
func WithLogger(logger logger.Logger) CryptoOption {
	return newFuncCryptoOption(func(c *cryptoConfig) {
		c.Log = logger
	})
}

func WithConfig(config config.Config) CryptoOption {
	return newFuncCryptoOption(func(c *cryptoConfig) {
		c.Config = config
	})
}

func WithConfigObserver(obj *config.ConfigObserver) CryptoOption {
	return newFuncCryptoOption(func(c *cryptoConfig) {
		c.ConfigObserver = obj
	})
}

func WithCypherLevel(lv int32) CryptoOption {
	return newFuncCryptoOption(func(c *cryptoConfig) {
		c.CypherLevel = lv
	})
}
