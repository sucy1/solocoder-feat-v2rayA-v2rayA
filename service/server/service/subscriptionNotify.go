package service

import (
	"sync"
	"time"

	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/core/v2ray"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

type SubscriptionUpdateEvent struct {
	ID            int       `json:"id"`
	Remarks       string    `json:"remarks"`
	Address       string    `json:"address"`
	ServerCount   int       `json:"serverCount"`
	PrevCount     int       `json:"prevCount"`
	AddedCount    int       `json:"addedCount"`
	RemovedCount  int       `json:"removedCount"`
	UpdatedAt     time.Time `json:"updatedAt"`
	Status        string    `json:"status"`
	Info          string    `json:"info"`
	Success       bool      `json:"success"`
	ErrorMessage  string    `json:"errorMessage,omitempty"`
}

type SubscriptionNotifyManager struct {
	mu          sync.Mutex
	notifyChan  chan *SubscriptionUpdateEvent
	ticker      *time.Ticker
	stopChan    chan struct{}
	running     bool
	lastUpdate  map[int]time.Time
}

var (
	notifyManager     *SubscriptionNotifyManager
	notifyManagerOnce sync.Once
)

func GetSubscriptionNotifyManager() *SubscriptionNotifyManager {
	notifyManagerOnce.Do(func() {
		notifyManager = &SubscriptionNotifyManager{
			notifyChan: make(chan *SubscriptionUpdateEvent, 100),
			lastUpdate: make(map[int]time.Time),
		}
	})
	return notifyManager
}

func (m *SubscriptionNotifyManager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return
	}

	setting := configure.GetSettingNotNil()
	if !setting.SubscriptionNotifyEnabled {
		return
	}

	m.running = true
	m.stopChan = make(chan struct{})

	interval := time.Duration(setting.SubscriptionNotifyIntervalHour) * time.Hour
	if interval < time.Hour {
		interval = time.Hour
	}

	m.ticker = time.NewTicker(interval)

	go func() {
		for {
			select {
			case <-m.stopChan:
				return
			case <-m.ticker.C:
				m.checkAndUpdateSubscriptions()
			}
		}
	}()

	m.checkAndUpdateSubscriptions()

	log.Info("Subscription notify manager started, interval: %v", interval)
}

func (m *SubscriptionNotifyManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	m.running = false
	if m.ticker != nil {
		m.ticker.Stop()
	}
	if m.stopChan != nil {
		close(m.stopChan)
	}

	log.Info("Subscription notify manager stopped")
}

func (m *SubscriptionNotifyManager) Restart() {
	m.Stop()

	setting := configure.GetSettingNotNil()
	if setting.SubscriptionNotifyEnabled {
		m.Start()
	}
}

func (m *SubscriptionNotifyManager) checkAndUpdateSubscriptions() {
	subs := configure.GetSubscriptions()

	for i, sub := range subs {
		event := m.updateSubscription(i)
		if event != nil {
			select {
			case m.notifyChan <- event:
			default:
				log.Warn("Subscription notify channel is full, dropping event")
			}

			m.lastUpdate[i] = time.Now()

			if v2ray.ApiFeed != nil {
				v2ray.ApiFeed.ProductMessage("subscription_update", event)
			}
		}
	}
}

func (m *SubscriptionNotifyManager) updateSubscription(index int) *SubscriptionUpdateEvent {
	subs := configure.GetSubscriptions()
	if index >= len(subs) {
		return nil
	}

	sub := subs[index]
	prevCount := len(sub.Servers)

	event := &SubscriptionUpdateEvent{
		ID:          index + 1,
		Remarks:     sub.Remarks,
		Address:     sub.Address,
		PrevCount:   prevCount,
		UpdatedAt:   time.Now(),
		Status:      sub.Status,
		Info:        sub.Info,
	}

	err := UpdateSubscription(index, false)
	if err != nil {
		event.Success = false
		event.ErrorMessage = err.Error()
		log.Warn("Auto update subscription failed: %v", err)
		return event
	}

	subs = configure.GetSubscriptions()
	if index < len(subs) {
		updatedSub := subs[index]
		event.ServerCount = len(updatedSub.Servers)
		event.Status = updatedSub.Status
		event.Info = updatedSub.Info
		event.Success = true

		if event.ServerCount > event.PrevCount {
			event.AddedCount = event.ServerCount - event.PrevCount
		}
		if event.ServerCount < event.PrevCount {
			event.RemovedCount = event.PrevCount - event.ServerCount
		}

		log.Info("Subscription updated successfully: %s, %d servers (prev: %d, added: %d, removed: %d)",
			updatedSub.Remarks, event.ServerCount, event.PrevCount, event.AddedCount, event.RemovedCount)
	}

	return event
}

func (m *SubscriptionNotifyManager) GetNotifyChannel() <-chan *SubscriptionUpdateEvent {
	return m.notifyChan
}

func (m *SubscriptionNotifyManager) CheckNow() {
	go m.checkAndUpdateSubscriptions()
}

func (m *SubscriptionNotifyManager) GetLastUpdateTime(index int) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.lastUpdate[index]; ok {
		return t
	}
	return time.Time{}
}

func GetSubscriptionNotifyStatus() map[string]interface{} {
	m := GetSubscriptionNotifyManager()
	setting := configure.GetSettingNotNil()

	return map[string]interface{}{
		"enabled":  setting.SubscriptionNotifyEnabled,
		"interval": setting.SubscriptionNotifyIntervalHour,
		"running":  m.running,
	}
}

func TriggerSubscriptionNotifyCheck() {
	m := GetSubscriptionNotifyManager()
	m.CheckNow()
}

func UpdateSubscriptionNotifySetting(enabled bool, intervalHours int) error {
	setting := configure.GetSettingNotNil()
	setting.SubscriptionNotifyEnabled = enabled
	setting.SubscriptionNotifyIntervalHour = intervalHours

	if err := configure.SetSetting(setting); err != nil {
		return err
	}

	m := GetSubscriptionNotifyManager()
	m.Restart()

	if setting.SubscriptionAutoUpdateMode == configure.AutoUpdateAtIntervals {
		conf.TickerUpdateSubscription.Reset(time.Duration(setting.SubscriptionAutoUpdateIntervalHour) * time.Hour)
	}

	return nil
}

func init() {
	v2ray.ApiProducts = append(v2ray.ApiProducts, "subscription_update")
}
