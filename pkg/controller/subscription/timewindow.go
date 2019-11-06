// Copyright 2019 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package subscription

import (
	"sort"
	"time"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	"k8s.io/klog"
)

// TODO
// type conversion and initialize to subscription level
// the idea is that the reconciler will be processed only if there a difference, so conversion won't be that hard

var CURDAY = time.Date(0000, 1, 1, 0, 0, 0, 0, time.UTC)
var MIDNIGHT = time.Date(0000, 1, 2, 0, 0, 0, 0, time.UTC)

// type TimeWindow struct {
// 	// if true/false, the subscription will only run or not run during this time window.
// 	WindowType string `json:"windowtype,omitempty"`
// 	// weekdays defined the day of the week for this time window
// 	//https://golang.org/pkg/time/#Weekday
// 	Location string         `json:"location,omitempty"`
// 	Weekdays []time.Weekday `json:"weekdays,omitempty"`
// 	Hours    []HourRange    `json:"hours,omitempty"`
// }

type RunHourRanges []appv1alpha1.HourRange

// thie field is actually int
type runDays []time.Weekday

func (r runDays) Len() int           { return len(r) }
func (r runDays) Less(i, j int) bool { return r[i] < r[j] }
func (r runDays) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }

func (r runDays) NextWeekdayToRun(t time.Time) time.Duration {
	// if weekdays is sorted, we want the next day with is greater than the t.Weekday
	// the weekdays is loop such as [3, 4, 5], if t==6, we should return 3 aka 4 days
	// if t == 2 then we should return 1

	if r.Len() == 0 {
		return time.Duration(0)
	}
	tc := t.Weekday()
	sort.Sort(r)

	var days int

	if tc > r[len(r)-1] {
		days = 7 - int(tc) + int(r[0])
	} else {
		for _, d := range r {
			if tc < d {
				days = int(d - tc)
				break
			}
		}
	}

	return time.Duration(days-1) * time.Hour * 24
}

func (rh RunHourRanges) Len() int { return len(rh) }
func (rh RunHourRanges) Less(i, j int) bool {
	s := parseTimeWithKitchenFormat(rh[i].Start)
	e := parseTimeWithKitchenFormat(rh[j].Start)
	return e.After(s)
}
func (rh RunHourRanges) Swap(i, j int) { rh[i], rh[j] = rh[j], rh[i] }

func parseTimeWithKitchenFormat(tstr string) time.Time {
	t, err := time.Parse(time.Kitchen, tstr)
	if err != nil {
		klog.Errorf("Can't parse time string %v with the time.Kitchen format %v, will use the current time instead ", tstr, time.Kitchen)
		return time.Now().UTC()
	}
	return t
}

func getLoc(loc string) *time.Location {
	l, err := time.LoadLocation(loc)
	if err != nil {
		local, _ := time.LoadLocation("Local")
		klog.Errorf("Failded to parse the location string %v, will use the current loc %v ", loc, local)
		return local
	}
	return l
}

func UnifyTimeZone(tw *appv1alpha1.TimeWindow, t time.Time) time.Time {
	loc := getLoc(tw.Location)
	return t.In(loc)
}

func NextStartPoint(tw *appv1alpha1.TimeWindow, t time.Time) time.Duration {
	// convert current time to the location time defined within the timewindow
	uniTime := UnifyTimeZone(tw, t)

	// TODO validate the hour ranges, to avoid the end time is earlier than start time
	vHr := validateHourRange(tw.Hours)
	if len(vHr) == 0 {
		return time.Duration(0)
	}
	// generate the duration for t
	return GenerateNextPoint(vHr, tw.Weekdays, uniTime)
}

func validateHourRange(rg []appv1alpha1.HourRange) RunHourRanges {
	h := RunHourRanges{}
	for _, r := range rg {
		s, e := parseTimeWithKitchenFormat(r.Start), parseTimeWithKitchenFormat(r.End)
		if s.Before(e) {
			h = append(h, r)
		}
	}
	return h
}

func sortRangeByStartTime(twHr RunHourRanges) RunHourRanges {
	rh := twHr
	sort.Sort(rh)
	return rh
}

// next time will be:
// if current time is bigger than the last time point of the window, nextTime will be weekdays offset + the hour offset
// if current time is smaller than the lastSlot time point, nextTime will be a duration till next time slot start point or a 0(if current time is within a time window)

func GenerateNextPoint(vhours RunHourRanges, wdays []time.Weekday, curTime time.Time) time.Duration {
	slots := sortRangeByStartTime(vhours)
	timeByHour := parseTimeWithKitchenFormat(curTime.Format(time.Kitchen))
	// t is greater than todays window
	// eg t is 11pm
	// slots [1, 3 pm]
	lastSlot := parseTimeWithKitchenFormat(slots[len(slots)-1].End)
	if lastSlot.Before(timeByHour) {

		nxtStart := parseTimeWithKitchenFormat(slots[0].Start)
		rdays := runDays(wdays)
		dayOffsets := rdays.NextWeekdayToRun(curTime)

		// Hour difference is from curret day to midnight, then midnight to slot[0]
		// Current time to the next day's midnight
		return MIDNIGHT.Sub(timeByHour) + nxtStart.Sub(CURDAY) + dayOffsets
	}

	// t is at time range
	for _, slot := range slots {
		slotStart, slotEnd := parseTimeWithKitchenFormat(slot.Start), parseTimeWithKitchenFormat(slot.End)
		// fmt.Println(slotStart.String(), slotEnd.String())
		if timeByHour.Before(slotStart) {
			return slotStart.Sub(timeByHour)
		} else if timeByHour.After(slotStart) && timeByHour.Before(slotEnd) {
			return time.Duration(0)
		}
	}
	return time.Duration(-1)
}
