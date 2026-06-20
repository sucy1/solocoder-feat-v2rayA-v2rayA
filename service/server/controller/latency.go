package controller

import (
	"time"

	"github.com/gin-gonic/gin"
	jsoniter "github.com/json-iterator/go"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/server/service"
)

func GetPingLatency(ctx *gin.Context) {
	updatingMu.Lock()
	if updating {
		common.ResponseError(ctx, processingErr)
		updatingMu.Unlock()
		return
	}
	updating = true
	updatingMu.Unlock()
	defer func() {
		updatingMu.Lock()
		updating = false
		updatingMu.Unlock()
	}()

	var wt []*configure.Which
	err := jsoniter.Unmarshal([]byte(ctx.Query("whiches")), &wt)
	if err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}
	wt, err = service.Ping(wt, 1*time.Second)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	common.ResponseSuccess(ctx, gin.H{
		"whiches": wt,
	})
}

func GetHttpLatency(ctx *gin.Context) {
	updatingMu.Lock()
	if updating {
		common.ResponseError(ctx, processingErr)
		updatingMu.Unlock()
		return
	}
	updating = true
	updatingMu.Unlock()
	defer func() {
		updatingMu.Lock()
		updating = false
		updatingMu.Unlock()
	}()

	var wt []*configure.Which
	err := jsoniter.Unmarshal([]byte(ctx.Query("whiches")), &wt)
	if err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}
	wt, err = service.TestHttpLatency(wt, 8*time.Second, 32, false, ctx.Query("testUrl"))
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	common.ResponseSuccess(ctx, gin.H{
		"whiches": wt,
	})
}

func PostLatencyTestAll(ctx *gin.Context) {
	var data struct {
		Type        string `json:"type"`
		Concurrency int    `json:"concurrency"`
		Timeout     int    `json:"timeout"`
		UseCache    bool   `json:"useCache"`
		TestURL     string `json:"testUrl"`
	}
	if err := ctx.ShouldBindJSON(&data); err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}

	config := service.DefaultLatencyConfig()
	if data.Concurrency > 0 {
		config.Concurrency = data.Concurrency
	}
	if data.Timeout > 0 {
		config.Timeout = time.Duration(data.Timeout) * time.Second
	}
	if data.TestURL != "" {
		config.TestURL = data.TestURL
	}

	testType := "tcp"
	if data.Type == "http" {
		testType = "http"
	}

	results, err := service.TestAllLatencies(config, testType, data.UseCache)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, gin.H{
		"results": results,
		"total":   len(results),
		"config":  config,
	})
}

func GetLatencyStatus(ctx *gin.Context) {
	testing, progress, total := service.GetLatencyStatus()
	common.ResponseSuccess(ctx, gin.H{
		"testing":  testing,
		"progress": progress,
		"total":    total,
	})
}

func DeleteLatencyCache(ctx *gin.Context) {
	service.ClearLatencyCache()
	common.ResponseSuccess(ctx, nil)
}
