package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/v2rayA/v2rayA/common/netTools/netstat"
	"github.com/v2rayA/v2rayA/core/v2ray"
	"github.com/v2rayA/v2rayA/db/configure"
)

type ConnectionStatsPoint struct {
	Timestamp    time.Time `json:"timestamp"`
	Connections  int       `json:"connections"`
	UploadBytes  uint64    `json:"uploadBytes"`
	DownloadBytes uint64   `json:"downloadBytes"`
	UploadSpeed  float64   `json:"uploadSpeed"`
	DownloadSpeed float64  `json:"downloadSpeed"`
}

type NodeConnectionStats struct {
	Which       *configure.Which        `json:"which"`
	ServerName  string                 `json:"serverName"`
	ServerAddr  string                 `json:"serverAddr"`
	OutboundTag string                 `json:"outboundTag"`
	Stats       []*ConnectionStatsPoint `json:"stats"`
	Current     *ConnectionStatsPoint  `json:"current"`
	TotalUpload uint64                 `json:"totalUpload"`
	TotalDownload uint64               `json:"totalDownload"`
}

type ConnectionStatsConfig struct {
	TimeRange   string        `json:"timeRange"`
	Interval    time.Duration `json:"interval"`
	MaxPoints   int           `json:"maxPoints"`
	DataRetentionDays int    `json:"dataRetentionDays"`
}

type statsDataPoint struct {
	timestamp     time.Time
	connections   int
	uploadBytes   uint64
	downloadBytes uint64
}

type nodeStatsHistory struct {
	mu          sync.RWMutex
	dataPoints  []*statsDataPoint
	lastUpload  uint64
	lastDownload uint64
}

var (
	statsCollectorMu   sync.Mutex
	statsCollectorRunning bool
	statsHistory       = make(map[string]*nodeStatsHistory)
	statsHistoryMu     sync.RWMutex
	collectorTicker    *time.Ticker
	stopCollector      chan struct{}
)

func DefaultStatsConfig() *ConnectionStatsConfig {
	return &ConnectionStatsConfig{
		TimeRange:         "5min",
		Interval:          10 * time.Second,
		MaxPoints:         42,
		DataRetentionDays: 7,
	}
}

func getNodeKey(which *configure.Which) string {
	return fmt.Sprintf("%s-%d-%d", which.TYPE, which.Sub, which.ID)
}

func getTimeRangeConfig(timeRange string) (interval time.Duration, maxPoints int) {
	switch timeRange {
	case "5min":
		return 10 * time.Second, 30
	case "1h":
		return 60 * time.Second, 60
	case "24h":
		return 5 * time.Minute, 288
	default:
		return 10 * time.Second, 30
	}
}

func StartStatsCollector() {
	statsCollectorMu.Lock()
	defer statsCollectorMu.Unlock()

	if statsCollectorRunning {
		return
	}

	statsCollectorRunning = true
	stopCollector = make(chan struct{})
	collectorTicker = time.NewTicker(10 * time.Second)

	go func() {
		for {
			select {
			case <-stopCollector:
				return
			case <-collectorTicker.C:
				collectConnectionStats()
			}
		}
	}()
}

func StopStatsCollector() {
	statsCollectorMu.Lock()
	defer statsCollectorMu.Unlock()

	if !statsCollectorRunning {
		return
	}

	collectorTicker.Stop()
	close(stopCollector)
	statsCollectorRunning = false
}

func collectConnectionStats() {
	if !v2ray.ProcessManager.Running() {
		return
	}

	css := configure.GetConnectedServers()
	if css == nil || css.Len() == 0 {
		return
	}

	conns, err := netstat.GetConnections()
	if err != nil {
		return
	}

	p := v2ray.ProcessManager.Process()
	if p == nil {
		return
	}

	tag2Which := make(map[string]*configure.Which)
	for _, which := range css.Get() {
		if idx, ok := p.tag2WhichIndex[which.Outbound]; ok {
			if idx < css.Len() {
				tag2Which[which.Outbound] = which
			}
		}
	}

	nodeConnections := make(map[string]int)
	for _, conn := range conns {
		for tag := range tag2Which {
			if conn.State == "ESTABLISHED" {
				nodeConnections[tag]++
			}
		}
	}

	now := time.Now()
	statsHistoryMu.Lock()
	defer statsHistoryMu.Unlock()

	for tag, which := range tag2Which {
		key := getNodeKey(which)

		history, ok := statsHistory[key]
		if !ok {
			history = &nodeStatsHistory{}
			statsHistory[key] = history
		}

		history.mu.Lock()

		point := &statsDataPoint{
			timestamp:   now,
			connections: nodeConnections[tag],
		}

		if len(history.dataPoints) > 0 {
			lastPoint := history.dataPoints[len(history.dataPoints)-1]
			interval := now.Sub(lastPoint.timestamp).Seconds()
			if interval > 0 {
				point.uploadBytes = history.lastUpload
				point.downloadBytes = history.lastDownload

				if history.lastUpload >= lastPoint.uploadBytes {
					_ = (history.lastUpload - lastPoint.uploadBytes)
				}
				if history.lastDownload >= lastPoint.downloadBytes {
					_ = (history.lastDownload - lastPoint.downloadBytes)
				}
			}
		}

		history.dataPoints = append(history.dataPoints, point)

		maxRetention := 7 * 24 * time.Hour
		cutoff := now.Add(-maxRetention)
		for len(history.dataPoints) > 0 && history.dataPoints[0].timestamp.Before(cutoff) {
			history.dataPoints = history.dataPoints[1:]
		}

		history.mu.Unlock()
	}
}

func GetNodeConnectionStats(which *configure.Which, timeRange string) (*NodeConnectionStats, error) {
	key := getNodeKey(which)

	statsHistoryMu.RLock()
	history, ok := statsHistory[key]
	statsHistoryMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no stats available for this node")
	}

	interval, maxPoints := getTimeRangeConfig(timeRange)

	history.mu.RLock()
	defer history.mu.RUnlock()

	var filteredPoints []*statsDataPoint
	cutoff := time.Now().Add(-parseTimeRange(timeRange))

	for _, p := range history.dataPoints {
		if p.timestamp.After(cutoff) {
			filteredPoints = append(filteredPoints, p)
		}
	}

	if len(filteredPoints) > maxPoints {
		step := len(filteredPoints) / maxPoints
		var sampled []*statsDataPoint
		for i := 0; i < len(filteredPoints); i += step {
			sampled = append(sampled, filteredPoints[i])
		}
		if len(sampled) > maxPoints {
			sampled = sampled[:maxPoints]
		}
		filteredPoints = sampled
	}

	statsPoints := make([]*ConnectionStatsPoint, 0, len(filteredPoints))
	var totalUpload, totalDownload uint64

	for i, p := range filteredPoints {
		sp := &ConnectionStatsPoint{
			Timestamp:     p.timestamp,
			Connections:   p.connections,
			UploadBytes:   p.uploadBytes,
			DownloadBytes: p.downloadBytes,
		}

		if i > 0 {
			prev := filteredPoints[i-1]
			dt := p.timestamp.Sub(prev.timestamp).Seconds()
			if dt > 0 {
				if p.uploadBytes >= prev.uploadBytes {
					sp.UploadSpeed = float64(p.uploadBytes-prev.uploadBytes) / dt
				}
				if p.downloadBytes >= prev.downloadBytes {
					sp.DownloadSpeed = float64(p.downloadBytes-prev.downloadBytes) / dt
				}
			}
		}

		statsPoints = append(statsPoints, sp)
		totalUpload = p.uploadBytes
		totalDownload = p.downloadBytes
	}

	var current *ConnectionStatsPoint
	if len(statsPoints) > 0 {
		current = statsPoints[len(statsPoints)-1]
	}

	serverName := ""
	serverAddr := ""
	if sr, err := which.LocateServerRaw(); err == nil && sr.ServerObj != nil {
		serverName = sr.ServerObj.GetName()
		serverAddr = fmt.Sprintf("%s:%d", sr.ServerObj.GetHostname(), sr.ServerObj.GetPort())
	}

	return &NodeConnectionStats{
		Which:         which,
		ServerName:    serverName,
		ServerAddr:    serverAddr,
		OutboundTag:   which.Outbound,
		Stats:         statsPoints,
		Current:       current,
		TotalUpload:   totalUpload,
		TotalDownload: totalDownload,
	}, nil
}

func parseTimeRange(timeRange string) time.Duration {
	switch timeRange {
	case "5min":
		return 5 * time.Minute
	case "1h":
		return time.Hour
	case "24h":
		return 24 * time.Hour
	default:
		return 5 * time.Minute
	}
}

func GetAllConnectionStats(timeRange string) ([]*NodeConnectionStats, error) {
	css := configure.GetConnectedServers()
	if css == nil || css.Len() == 0 {
		return []*NodeConnectionStats{}, nil
	}

	var stats []*NodeConnectionStats
	for _, which := range css.Get() {
		nodeStats, err := GetNodeConnectionStats(which, timeRange)
		if err == nil {
			stats = append(stats, nodeStats)
		}
	}

	return stats, nil
}

func GetConnectionStatsSummary() (map[string]interface{}, error) {
	stats, err := GetAllConnectionStats("5min")
	if err != nil {
		return nil, err
	}

	var totalConnections int
	var totalUpload, totalDownload uint64
	var totalUploadSpeed, totalDownloadSpeed float64

	for _, s := range stats {
		totalConnections += s.Current.Connections
		totalUpload += s.TotalUpload
		totalDownload += s.TotalDownload
		if s.Current != nil {
			totalUploadSpeed += s.Current.UploadSpeed
			totalDownloadSpeed += s.Current.DownloadSpeed
		}
	}

	return map[string]interface{}{
		"totalConnections":  totalConnections,
		"totalUpload":       totalUpload,
		"totalDownload":     totalDownload,
		"totalUploadSpeed":  totalUploadSpeed,
		"totalDownloadSpeed": totalDownloadSpeed,
		"activeNodes":       len(stats),
	}, nil
}

func UpdateTrafficBytes(which *configure.Which, upload, download uint64) {
	key := getNodeKey(which)

	statsHistoryMu.Lock()
	defer statsHistoryMu.Unlock()

	history, ok := statsHistory[key]
	if !ok {
		history = &nodeStatsHistory{}
		statsHistory[key] = history
	}

	history.mu.Lock()
	defer history.mu.Unlock()

	history.lastUpload = upload
	history.lastDownload = download
}
