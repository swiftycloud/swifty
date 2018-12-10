/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package dbscr

import (
	"time"
	"strings"
	"strconv"
)

func NextPeriod(since *time.Time, period string) time.Time {
	/* Common sane types */
	switch period {
	case "hourly":
		return since.Add(time.Hour)
	case "daily":
		return since.AddDate(0, 0, 1)
	case "weekly":
		return since.AddDate(0, 0, 7)
	case "monthly":
		return since.AddDate(0, 1, 0)
	}

	/* For debugging mostly */
	var mult time.Duration
	var dur string
	if strings.HasSuffix(period, "s") {
		dur = strings.TrimSuffix(period, "s")
		mult = time.Second
	} else if strings.HasSuffix(period, "m") {
		dur = strings.TrimSuffix(period, "s")
		mult = time.Minute
	}

	if mult != 0 {
		i, err := strconv.Atoi(dur)
		if err != nil {
			goto out
		}
		return since.Add(time.Duration(i) * mult)
	}
out:
	panic("Bad period value: " + period)
}

