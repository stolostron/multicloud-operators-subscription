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

// TimeWindow defines a time window for subscription to run or be blocked
// type TimeWindow struct {
// 	// if true/false, the subscription will only run or not run during this time window.
// 	WindowType string `json:"windowtype,omitempty"`
// 	Location   string `json:"location,omitempty"`
// 	// weekdays defined the day of the week for this time window https://golang.org/pkg/time/#Weekday
// 	Weekdays []time.Weekday `json:"weekdays,omitempty"`
// 	Hours    []HourRange    `json:"hours,omitempty"`
// }

// type HourRange struct {
// 	//Kitchen format defined at https://golang.org/pkg/time/#pkg-constants
// 	// +kubebuilder:validation:Pattern=([0-1][0-9])\:([0-5][0-9])([A|P]+)[M]
// 	Start string `json:"start"`
// 	// +kubebuilder:validation:Pattern=([0-1][0-9])\:([0-5][0-9])([A|P]+)[M]
// 	End string `json:"end"`
// }

var CURDAY = time.Date(0000, 1, 1, 0, 0, 0, 0, time.UTC)
var MIDNIGHT = time.Date(0000, 1, 2, 0, 0, 0, 0, time.UTC)

type RunHourRanges []appv1alpha1.HourRange

// thie field is actually int
type runDays []time.Weekday

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

func UnifyTimeZone(tw *appv1alpha1.TimeWindow, t time.Time) time.Time {
	loc := getLoc(tw.Location)
	return t.In(loc)
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

func parseTimeWithKitchenFormat(tstr string) time.Time {
	t, err := time.Parse(time.Kitchen, tstr)
	if err != nil {
		klog.Errorf("Can't parse time string %v with the time.Kitchen format %v, will use the current time instead ", tstr, time.Kitchen)
		return time.Now().UTC()
	}
	return t
}

func sortRangeByStartTime(twHr RunHourRanges) RunHourRanges {
	rh := twHr
	sort.Sort(rh)
	return rh
}

// next time will be:
// if current time is bigger than the last time point of the window, nextTime will be weekdays offset + the hour offset
// if current time is smaller than the lastSlot time point, nextTime will be a duration till next time slot start point or a 0(if current time is within a time window)

func GenerateNextPoint(vhours RunHourRanges, wdays []time.Weekday, uniTime time.Time) time.Duration {
	slots := sortRangeByStartTime(vhours)
	timeByHour := parseTimeWithKitchenFormat(uniTime.Format(time.Kitchen))
	// t is greater than todays window
	// eg t is 11pm
	// slots [1, 3 pm]
	lastSlot := parseTimeWithKitchenFormat(slots[len(slots)-1].End)
	if lastSlot.Before(timeByHour) {

		nxtStart := parseTimeWithKitchenFormat(slots[0].Start)
		rdays := runDays(wdays)
		dayOffsets := rdays.DurationToNextRunableWeekday(uniTime)

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

// type runDays []time.Weekday

func (r runDays) Len() int           { return len(r) }
func (r runDays) Less(i, j int) bool { return r[i] < r[j] }
func (r runDays) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }

func (r runDays) DurationToNextRunableWeekday(t time.Time) time.Duration {
	// if weekdays is sorted, we want the next day with is greater than the t.Weekday
	// the weekdays is loop such as [3, 4, 5], if t==6, we should return 3 aka 4 days
	// if t == 2 then we should return 1

	if r.Len() == 0 {
		// this mean you will wait for less than a day.
		return time.Duration(0)
	}

	tc := t.Weekday()

	sort.Sort(r)

	var days int

	//if the current weekday is later than the latest weekday in the time window,
	//then we find the days left till first runable weekday
	if tc > r[len(r)-1] {
		daysLeftOfThisWeek := 7 - int(tc)
		mostRecentWeekday := int(r[0])
		days = daysLeftOfThisWeek + mostRecentWeekday
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
