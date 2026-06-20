package controller

import (
	"time"

	"github.com/gin-gonic/gin"
	jsoniter "github.com/json-iterator/go"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/server/service"
)

func GetConnectionStats(ctx *gin.Context) {
	timeRange := ctx.DefaultQuery("timeRange", "5min")

	stats, err := service.GetAllConnectionStats(timeRange)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, gin.H{
		"stats":     stats,
		"timeRange": timeRange,
		"timestamp": time.Now(),
	})
}

func GetNodeConnectionStats(ctx *gin.Context) {
	var which configure.Which
	err := jsoniter.Unmarshal([]byte(ctx.Query("which")), &which)
	if err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}

	timeRange := ctx.DefaultQuery("timeRange", "5min")

	stats, err := service.GetNodeConnectionStats(&which, timeRange)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, gin.H{
		"stats":     stats,
		"timeRange": timeRange,
		"timestamp": time.Now(),
	})
}

func GetConnectionStatsSummary(ctx *gin.Context) {
	summary, err := service.GetConnectionStatsSummary()
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, summary)
}

func PostStartStatsCollector(ctx *gin.Context) {
	service.StartStatsCollector()
	common.ResponseSuccess(ctx, nil)
}

func PostStopStatsCollector(ctx *gin.Context) {
	service.StopStatsCollector()
	common.ResponseSuccess(ctx, nil)
}

func GetStatsConfig(ctx *gin.Context) {
	config := service.DefaultStatsConfig()
	common.ResponseSuccess(ctx, gin.H{
		"config": config,
	})
}
