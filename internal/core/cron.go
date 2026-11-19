package core

// Cron thật cho WordPress (mục 6.10). Hợp đồng ở types_m9.go. File tạm của
// đợt 0: thợ đợt 2 thay thân hàm và thêm siteCronSteps, siteCronAbsent.

import (
	"context"

	"github.com/doxuta/tpan/internal/validate"
)

func (c *Cron) List(context.Context) (CronOverview, error)            { return CronOverview{}, errM9 }
func (c *Cron) Run(context.Context, validate.Domain) (CronRun, error) { return CronRun{}, errM9 }
