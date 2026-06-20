package service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/v2rayA/v2rayA/core/v2ray"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

type LatencyTestConfig struct {
	Concurrency int           `json:"concurrency"`
	Timeout     time.Duration `json:"timeout"`
	CacheTTL    time.Duration `json:"cache_ttl"`
	TestURL     string        `json:"test_url"`
}

type LatencyTestResult struct {
	Which       *configure.Which `json:"which"`
	TcpLatency  string           `json:"tcp_latency"`
	HttpLatency string           `json:"http_latency"`
	TestTime    time.Time        `json:"test_time"`
	FromCache   bool             `json:"from_cache"`
}

type latencyCacheEntry struct {
	result    *LatencyTestResult
	expiresAt time.Time
}

var (
	latencyCache     = make(map[string]*latencyCacheEntry)
	latencyCacheMu   sync.RWMutex
	latencyTestMu    sync.Mutex
	isLatencyTesting bool
)

func DefaultLatencyConfig() *LatencyTestConfig {
	return &LatencyTestConfig{
		Concurrency: 5,
		Timeout:     3 * time.Second,
		CacheTTL:    5 * time.Minute,
		TestURL:     HttpTestURL,
	}
}

func getCacheKey(w *configure.Which, testType string) string {
	return fmt.Sprintf("%s-%d-%d-%s", w.TYPE, w.Sub, w.ID, testType)
}

func getCachedResult(w *configure.Which, testType string) *LatencyTestResult {
	latencyCacheMu.RLock()
	defer latencyCacheMu.RUnlock()

	key := getCacheKey(w, testType)
	if entry, ok := latencyCache[key]; ok {
		if time.Now().Before(entry.expiresAt) {
			result := *entry.result
			result.FromCache = true
			return &result
		}
	}
	return nil
}

func setCachedResult(w *configure.Which, testType string, result *LatencyTestResult, ttl time.Duration) {
	latencyCacheMu.Lock()
	defer latencyCacheMu.Unlock()

	key := getCacheKey(w, testType)
	latencyCache[key] = &latencyCacheEntry{
		result:    result,
		expiresAt: time.Now().Add(ttl),
	}
}

func getAllServers() []*configure.Which {
	var whiches []*configure.Which

	servers := configure.GetServers()
	for i := range servers {
		whiches = append(whiches, &configure.Which{
			TYPE: configure.ServerType,
			ID:   i + 1,
		})
	}

	subs := configure.GetSubscriptions()
	for subIdx := range subs {
		for srvIdx := range subs[subIdx].Servers {
			whiches = append(whiches, &configure.Which{
				TYPE: configure.SubscriptionServerType,
				Sub:  subIdx,
				ID:   srvIdx + 1,
			})
		}
	}

	return whiches
}

func TestAllLatencies(config *LatencyTestConfig, testType string, useCache bool) ([]*LatencyTestResult, error) {
	latencyTestMu.Lock()
	if isLatencyTesting {
		latencyTestMu.Unlock()
		return nil, fmt.Errorf("latency test is already in progress")
	}
	isLatencyTesting = true
	latencyTestMu.Unlock()

	defer func() {
		latencyTestMu.Lock()
		isLatencyTesting = false
		latencyTestMu.Unlock()
	}()

	if config == nil {
		config = DefaultLatencyConfig()
	}

	whiches := getAllServers()
	results := make([]*LatencyTestResult, 0, len(whiches))
	resultsMu := sync.Mutex{}

	if useCache {
		for _, w := range whiches {
			if cached := getCachedResult(w, testType); cached != nil {
				results = append(results, cached)
			}
		}
	}

	var toTest []*configure.Which
	for _, w := range whiches {
		if !useCache || getCachedResult(w, testType) == nil {
			toTest = append(toTest, w)
		}
	}

	if len(toTest) == 0 {
		sortResults(results)
		return results, nil
	}

	effectiveConcurrency := config.Concurrency
	if len(toTest) < effectiveConcurrency {
		effectiveConcurrency = len(toTest)
	}

	sem := make(chan struct{}, effectiveConcurrency)
	wg := sync.WaitGroup{}
	wg.Add(len(toTest))

	v2rayRunning := v2ray.ProcessManager.Running()
	var testFunc func(*configure.Which) *LatencyTestResult

	if testType == "tcp" {
		v2ray.ProcessManager.CheckAndStopTransparentProxy(nil)
		defer func() {
			if e := v2ray.ProcessManager.CheckAndSetupTransparentProxy(true, nil, v2ray.ProcessManager.GetRunningTemplate()); e != nil {
				log.Warn("TestAllLatencies: %v", e)
			}
		}()

		testFunc = func(w *configure.Which) *LatencyTestResult {
			result := &LatencyTestResult{
				Which:    w,
				TestTime: time.Now(),
			}
			_ = w.Ping(config.Timeout)
			result.TcpLatency = w.Latency
			return result
		}
	} else {
		var httpWhiches []*configure.Which
		for _, w := range toTest {
			cp := *w
			httpWhiches = append(httpWhiches, &cp)
		}

		_, err := TestHttpLatency(httpWhiches, config.Timeout, effectiveConcurrency, false, config.TestURL)
		if err != nil {
			return nil, err
		}

		for i, w := range toTest {
			result := &LatencyTestResult{
				Which:       w,
				HttpLatency: httpWhiches[i].Latency,
				TestTime:    time.Now(),
			}
			setCachedResult(w, "http", result, config.CacheTTL)
			resultsMu.Lock()
			results = append(results, result)
			resultsMu.Unlock()
		}

		if v2rayRunning && configure.GetConnectedServers() != nil {
			_ = v2ray.UpdateV2RayConfig()
		} else {
			v2ray.ProcessManager.Stop(true)
		}

		sortResults(results)
		return results, nil
	}

	for _, w := range toTest {
		sem <- struct{}{}
		go func(which *configure.Which) {
			defer wg.Done()
			defer func() { <-sem }()

			result := testFunc(which)
			setCachedResult(which, "tcp", result, config.CacheTTL)

			resultsMu.Lock()
			results = append(results, result)
			resultsMu.Unlock()
		}(w)
	}

	wg.Wait()

	sortResults(results)
	return results, nil
}

func parseLatency(lat string) int {
	if !strings.HasSuffix(lat, "ms") {
		return 999999
	}
	ms, err := strconv.Atoi(strings.TrimSuffix(lat, "ms"))
	if err != nil {
		return 999999
	}
	return ms
}

func sortResults(results []*LatencyTestResult) {
	sort.Slice(results, func(i, j int) bool {
		var li, lj int
		if results[i].TcpLatency != "" {
			li = parseLatency(results[i].TcpLatency)
			lj = parseLatency(results[j].TcpLatency)
		} else {
			li = parseLatency(results[i].HttpLatency)
			lj = parseLatency(results[j].HttpLatency)
		}
		return li < lj
	})
}

func GetLatencyStatus() (testing bool, progress int, total int) {
	latencyTestMu.Lock()
	defer latencyTestMu.Unlock()
	return isLatencyTesting, 0, 0
}

func ClearLatencyCache() {
	latencyCacheMu.Lock()
	defer latencyCacheMu.Unlock()
	latencyCache = make(map[string]*latencyCacheEntry)
}
