package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/server/service"
)

func GetSubscriptionNotifyStatus(ctx *gin.Context) {
	status := service.GetSubscriptionNotifyStatus()
	common.ResponseSuccess(ctx, status)
}

func PutSubscriptionNotifySetting(ctx *gin.Context) {
	var data struct {
		Enabled       bool `json:"enabled"`
		IntervalHours int  `json:"intervalHours"`
	}
	err := ctx.ShouldBindJSON(&data)
	if err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}

	if data.IntervalHours < 1 {
		data.IntervalHours = 1
	}

	err = service.UpdateSubscriptionNotifySetting(data.Enabled, data.IntervalHours)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, nil)
}

func PostSubscriptionNotifyCheck(ctx *gin.Context) {
	service.TriggerSubscriptionNotifyCheck()
	common.ResponseSuccess(ctx, nil)
}

func PostSubscriptionNotifyStart(ctx *gin.Context) {
	m := service.GetSubscriptionNotifyManager()
	m.Start()
	common.ResponseSuccess(ctx, nil)
}

func PostSubscriptionNotifyStop(ctx *gin.Context) {
	m := service.GetSubscriptionNotifyManager()
	m.Stop()
	common.ResponseSuccess(ctx, nil)
}
