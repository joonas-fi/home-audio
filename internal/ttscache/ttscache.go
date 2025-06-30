// Text-to-speech cache
package ttscache

import (
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/joonas-fi/home-audio/internal/bytespromise"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	cacheExpiration = 10 * time.Minute
)

var (
	hitsMetric = promauto.NewCounter(prometheus.CounterOpts{
		Name: "homeaudio_ttscache_hits",
		Help: "Cache hits",
	})
	missesMetric = promauto.NewCounter(prometheus.CounterOpts{
		Name: "homeaudio_ttscache_misses",
		Help: "Cache misses",
	})
)

func New() *ttsCache {
	return &ttsCache{
		cache: map[string]*cacheItem{},
	}
}

type cacheItem struct {
	promise *bytespromise.Promise
	ttl     time.Time
}

type ttsCache struct {
	cache   map[string]*cacheItem
	cacheMu sync.Mutex
}

// three possibilities:
// 1. not cached
// 2. cached item is being produced (let's say takes a second)
// 3. cached item has been produced completely
func (s *ttsCache) Get(key string, generator func(wc io.WriteCloser) error) io.Reader {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	item, has := s.cache[key]
	if has { // was cached
		hitsMetric.Inc()
		return item.promise.NewReader()
	}

	missesMetric.Inc()

	// not cached. first one needs to produce the item
	promise := bytespromise.New()
	go func() {
		if err := generator(promise.NewWriter()); err != nil {
			slog.Error("ttsCache", "err", err)
		}
	}()
	s.cache[key] = &cacheItem{
		promise: promise,
		ttl:     time.Now().Add(cacheExpiration),
	}
	return promise.NewReader()
}

// purges old items from the cache
func (s *ttsCache) PurgeOldItems() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	slog.Debug("PurgeOldItems starting")

	now := time.Now()

	for key, item := range s.cache {
		if now.After(item.ttl) {
			delete(s.cache, key)
			slog.Debug("PurgeOldItems", "key", key)
		}
	}
}
