package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/v2rayA/v2rayA/common/netTools/netstat"
	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/core/v2ray"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

type ConnectionStatsPoint struct {
	Timestamp     time.Time `json:"timestamp"`
	Connections   int       `json:"connections"`
	UploadBytes   uint64    `json:"uploadBytes"`
	DownloadBytes uint64    `json:"downloadBytes"`
	UploadSpeed   float64   `json:"uploadSpeed"`
	DownloadSpeed float64   `json:"downloadSpeed"`
}

type NodeConnectionStats struct {
	Which         *configure.Which        `json:"which"`
	ServerName    string                 `json:"serverName"`
	ServerAddr    string                 `json:"serverAddr"`
	OutboundTag   string                 `json:"outboundTag"`
	Stats         []*ConnectionStatsPoint `json:"stats"`
	Current       *ConnectionStatsPoint  `json:"current"`
	TotalUpload   uint64                 `json:"totalUpload"`
	TotalDownload uint64                 `json:"totalDownload"`
}

type ConnectionStatsConfig struct {
	TimeRange         string        `json:"timeRange"`
	Interval          time.Duration `json:"interval"`
	MaxPoints         int           `json:"maxPoints"`
	DataRetentionDays int           `json:"dataRetentionDays"`
	PersistEnabled    bool          `json:"persistEnabled"`
	PersistInterval   time.Duration `json:"persistInterval"`
}

type statsDataPoint struct {
	timestamp     time.Time
	connections   int
	uploadBytes   uint64
	downloadBytes uint64
}

type persistedStatsPoint struct {
	Timestamp     time.Time `json:"timestamp"`
	Connections   int       `json:"connections"`
	UploadBytes   uint64    `json:"uploadBytes"`
	DownloadBytes uint64    `json:"downloadBytes"`
}

type persistedNodeStats struct {
	NodeType     configure.TouchType   `json:"nodeType"`
	SubID        int                    `json:"subId"`
	NodeID       int                    `json:"nodeId"`
	ServerName   string                 `json:"serverName"`
	ServerAddr   string                 `json:"serverAddr"`
	OutboundTag  string                 `json:"outboundTag"`
	DataPoints   []persistedStatsPoint  `json:"dataPoints"`
	LastUpload   uint64                 `json:"lastUpload"`
	LastDownload uint64                 `json:"lastDownload"`
}

type persistedStatsData struct {
	Version   int                  `json:"version"`
	Timestamp time.Time            `json:"timestamp"`
	Nodes     []persistedNodeStats `json:"nodes"`
}

type nodeStatsHistory struct {
	mu           sync.RWMutex
	dataPoints   []*statsDataPoint
	lastUpload   uint64
	lastDownload uint64
	serverName   string
	serverAddr   string
}

var (
	statsCollectorMu      sync.Mutex
	statsCollectorRunning bool
	statsHistory          = make(map[string]*nodeStatsHistory)
	statsHistoryMu        sync.RWMutex
	collectorTicker       *time.Ticker
	persistTicker         *time.Ticker
	stopCollector         chan struct{}
	statsLoaded           bool
)

const (
	statsFileName    = "connection_stats.json"
	statsDataVersion = 1
)

func DefaultStatsConfig() *ConnectionStatsConfig {
	return &ConnectionStatsConfig{
		TimeRange:         "5min",
		Interval:          10 * time.Second,
		MaxPoints:         42,
		DataRetentionDays: 7,
		PersistEnabled:    true,
		PersistInterval:   5 * time.Minute,
	}
}

func getNodeKey(which *configure.Which) string {
	return fmt.Sprintf("%s-%d-%d", which.TYPE, which.Sub, which.ID)
}

func getStatsFilePath() string {
	configPath := conf.GetEnvironmentConfig().Config
	return filepath.Join(configPath, statsFileName)
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

func loadStatsFromDisk() error {
	statsHistoryMu.Lock()
	defer statsHistoryMu.Unlock()

	if statsLoaded {
		return nil
	}

	filePath := getStatsFilePath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			statsLoaded = true
			return nil
		}
		return fmt.Errorf("failed to read stats file: %w", err)
	}

	var persisted persistedStatsData
	if err := json.Unmarshal(data, &persisted); err != nil {
		log.Warn("Failed to parse stats file, starting fresh: %v", err)
		statsLoaded = true
		return nil
	}

	cutoff := time.Now().AddDate(0, 0, -DefaultStatsConfig().DataRetentionDays)

	for _, node := range persisted.Nodes {
		key := fmt.Sprintf("%s-%d-%d", node.NodeType, node.SubID, node.NodeID)

		history := &nodeStatsHistory{
			lastUpload:   node.LastUpload,
			lastDownload: node.LastDownload,
			serverName:   node.ServerName,
			serverAddr:   node.ServerAddr,
		}

		for _, p := range node.DataPoints {
			if p.Timestamp.After(cutoff) {
				history.dataPoints = append(history.dataPoints, &statsDataPoint{
					timestamp:     p.Timestamp,
					connections:   p.Connections,
					uploadBytes:   p.UploadBytes,
					downloadBytes: p.DownloadBytes,
				})
			}
		}

		if len(history.dataPoints) > 0 {
			statsHistory[key] = history
		}
	}

	statsLoaded = true
	log.Info("Loaded connection stats from disk: %d nodes", len(persisted.Nodes))
	return nil
}

func parseNodeKey(key string) (nodeType configure.TouchType, subID int, nodeID int, ok bool) {
	parts := strings.Split(key, "-")
	if len(parts) < 3 {
		return "", 0, 0, false
	}
	nodeType = configure.TouchType(strings.Join(parts[:len(parts)-2], "-"))
	var err error
	subID, err = strconv.Atoi(parts[len(parts)-2])
	if err != nil {
		return "", 0, 0, false
	}
	nodeID, err = strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return "", 0, 0, false
	}
	return nodeType, subID, nodeID, true
}

func saveStatsToDisk() error {
	statsHistoryMu.RLock()
	defer statsHistoryMu.RUnlock()

	var persisted persistedStatsData
	persisted.Version = statsDataVersion
	persisted.Timestamp = time.Now()

	cutoff := time.Now().AddDate(0, 0, -DefaultStatsConfig().DataRetentionDays)

	for key, history := range statsHistory {
		history.mu.RLock()

		if len(history.dataPoints) == 0 {
			history.mu.RUnlock()
			continue
		}

		nodeType, subID, nodeID, ok := parseNodeKey(key)
		if !ok {
			history.mu.RUnlock()
			continue
		}

		nodeStats := persistedNodeStats{
			NodeType:     nodeType,
			SubID:        subID,
			NodeID:       nodeID,
			ServerName:   history.serverName,
			ServerAddr:   history.serverAddr,
			LastUpload:   history.lastUpload,
			LastDownload: history.lastDownload,
		}

		for _, p := range history.dataPoints {
			if p.timestamp.After(cutoff) {
				nodeStats.DataPoints = append(nodeStats.DataPoints, persistedStatsPoint{
					Timestamp:     p.timestamp,
					Connections:   p.connections,
					UploadBytes:   p.uploadBytes,
					DownloadBytes: p.downloadBytes,
				})
			}
		}

		if len(nodeStats.DataPoints) > 0 {
			persisted.Nodes = append(persisted.Nodes, nodeStats)
		}

		history.mu.RUnlock()
	}

	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}

	filePath := getStatsFilePath()
	tmpPath := filePath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write stats file: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("failed to rename stats file: %w", err)
	}

	log.Debug("Saved connection stats to disk: %d nodes", len(persisted.Nodes))
	return nil
}

func StartStatsCollector() {
	statsCollectorMu.Lock()
	defer statsCollectorMu.Unlock()

	if statsCollectorRunning {
		return
	}

	if !statsLoaded {
		if err := loadStatsFromDisk(); err != nil {
			log.Warn("Failed to load stats from disk: %v", err)
		}
	}

	statsCollectorRunning = true
	stopCollector = make(chan struct{})
	collectorTicker = time.NewTicker(10 * time.Second)
	persistTicker = time.NewTicker(5 * time.Minute)

	go func() {
		for {
			select {
			case <-stopCollector:
				return
			case <-collectorTicker.C:
				collectConnectionStats()
			case <-persistTicker.C:
				if err := saveStatsToDisk(); err != nil {
					log.Warn("Failed to persist stats: %v", err)
				}
			}
		}
	}()

	log.Info("Connection stats collector started")
}

func StopStatsCollector() {
	statsCollectorMu.Lock()
	defer statsCollectorMu.Unlock()

	if !statsCollectorRunning {
		return
	}

	collectorTicker.Stop()
	if persistTicker != nil {
		persistTicker.Stop()
	}
	close(stopCollector)
	statsCollectorRunning = false

	if err := saveStatsToDisk(); err != nil {
		log.Warn("Failed to save stats on stop: %v", err)
	}

	log.Info("Connection stats collector stopped")
}

func collectConnectionStats() {
	if !v2ray.ProcessManager.Running() {
		return
	}

	css := configure.GetConnectedServers()
	if css == nil || css.Len() == 0 {
		return
	}

	protocols := []string{"tcp", "udp"}
	portMap, err := netstat.ToPortMap(protocols)
	if err != nil {
		return
	}

	totalConns := 0
	for _, protoPorts := range portMap {
		for _, sockets := range protoPorts {
			for _, sock := range sockets {
				if sock.State == netstat.Established {
					totalConns++
				}
			}
		}
	}

	perNodeConns := 0
	if css.Len() > 0 {
		perNodeConns = totalConns / css.Len()
	}

	now := time.Now()
	statsHistoryMu.Lock()
	defer statsHistoryMu.Unlock()

	for _, which := range css.Get() {
		key := getNodeKey(which)

		history, ok := statsHistory[key]
		if !ok {
			history = &nodeStatsHistory{}
			statsHistory[key] = history
		}

		history.mu.Lock()

		if history.serverName == "" || history.serverAddr == "" {
			if sr, err := which.LocateServerRaw(); err == nil && sr.ServerObj != nil {
				history.serverName = sr.ServerObj.GetName()
				history.serverAddr = fmt.Sprintf("%s:%d", sr.ServerObj.GetHostname(), sr.ServerObj.GetPort())
			}
		}

		point := &statsDataPoint{
			timestamp:     now,
			connections:   perNodeConns,
			uploadBytes:   history.lastUpload,
			downloadBytes: history.lastDownload,
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
	_ = interval

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

	serverName := history.serverName
	serverAddr := history.serverAddr
	if serverName == "" {
		if sr, err := which.LocateServerRaw(); err == nil && sr.ServerObj != nil {
			serverName = sr.ServerObj.GetName()
			serverAddr = fmt.Sprintf("%s:%d", sr.ServerObj.GetHostname(), sr.ServerObj.GetPort())
		}
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
		if s.Current != nil {
			totalConnections += s.Current.Connections
			totalUploadSpeed += s.Current.UploadSpeed
			totalDownloadSpeed += s.Current.DownloadSpeed
		}
		totalUpload += s.TotalUpload
		totalDownload += s.TotalDownload
	}

	return map[string]interface{}{
		"totalConnections":   totalConnections,
		"totalUpload":        totalUpload,
		"totalDownload":      totalDownload,
		"totalUploadSpeed":   totalUploadSpeed,
		"totalDownloadSpeed": totalDownloadSpeed,
		"activeNodes":        len(stats),
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

func ForceSaveStats() error {
	return saveStatsToDisk()
}

func GetStatsPersistStatus() map[string]interface{} {
	statsHistoryMu.RLock()
	defer statsHistoryMu.RUnlock()

	nodeCount := len(statsHistory)
	var totalPoints int
	for _, h := range statsHistory {
		h.mu.RLock()
		totalPoints += len(h.dataPoints)
		h.mu.RUnlock()
	}

	return map[string]interface{}{
		"loaded":        statsLoaded,
		"nodeCount":     nodeCount,
		"totalPoints":   totalPoints,
		"persistFile":   getStatsFilePath(),
		"retentionDays": DefaultStatsConfig().DataRetentionDays,
	}
}
