package dbmodule

import (
	"time"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
)

func startMaintenanceTasks() {
	module.mgr.Go("basic maintenance", maintainBasic).Repeat(10 * time.Minute).MaxDelay(10 * time.Minute)
	module.mgr.Go("thorough maintenance", maintainThorough).Repeat(1 * time.Hour).MaxDelay(1 * time.Hour)
	module.mgr.Go("record maintenance", maintainRecords).Repeat(1 * time.Hour).MaxDelay(1 * time.Hour)
}

func maintainBasic(ctx mgr.WorkerCtx) error {
	log.Infof("database: running Maintain")
	return database.Maintain(ctx.Ctx())
}

func maintainThorough(ctx mgr.WorkerCtx) error {
	log.Infof("database: running MaintainThorough")
	return database.MaintainThorough(ctx.Ctx())
}

func maintainRecords(ctx mgr.WorkerCtx) error {
	log.Infof("database: running MaintainRecordStates")
	return database.MaintainRecordStates(ctx.Ctx())
}
