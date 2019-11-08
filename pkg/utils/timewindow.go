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

package utils

import (
	"sort"
	"strings"
	"time"

	"k8s.io/klog"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
)

var CURDAY, _ = time.Parse(time.UnixDate, "Sat Jan  1 00:00:00 UTC 0000")
var MIDNIGHT, _ = time.Parse(time.UnixDate, "Sun Jan  2 00:00:00 UTC 0000")

type RunHourRanges []appv1alpha1.HourRange

// thie field is actually int
type runDays []time.Weekday

//NextStartPoint will map the container's time to the location time specified by user
// then it will handle the window type as will the hour ange and weekdays
// for hour range and weekdays, it will handle as the following
// if hour range is empty and weekday is empty then retrun 0
// if hour range is empty and weekday is not then return nextday durtion(here the window type will be considered again)
func NextStartPoint(tw *appv1alpha1.TimeWindow, t time.Time) time.Duration {
	// convert current time to the location time defined within the timewindow
	uniTime := UnifyTimeZone(tw, t)
	klog.V(5).Infof("Time window checking at %v", uniTime.String())

	// valid hour ranges, meaning the each hour range's start time is earlier than the end time
	// also there's no overlap between 2 ranges
	vHr := validateHourRange(tw.Hours)

	rDays, rveDays := validateWeekDaysSlice(tw.Weekdays)

	if tw.WindowType != "" && tw.WindowType != "active" {
		// reverse slots
		rvevHr := ReverseRange(vHr)
		return GenerateNextPoint(rvevHr, rveDays, uniTime)
	}

	// generate the duration for t
	return GenerateNextPoint(vHr, rDays, uniTime)
}

func UnifyTimeZone(tw *appv1alpha1.TimeWindow, t time.Time) time.Time {
	loc := getLoc(tw.Location)
	return t.In(loc)
}

func getLoc(loc string) *time.Location {
	l, err := time.LoadLocation(loc)
	if err != nil {
		local, err := time.LoadLocation("Local")
		klog.Errorf("Error %v while parsing the location string %v, will use the current loc %v ", err, loc, local)

		return local
	}

	return l
}

func validateHourRange(rg []appv1alpha1.HourRange) RunHourRanges {
	if len(rg) == 0 {
		return rg
	}

	h := RunHourRanges{}

	for _, r := range rg {
		s, e := parseTimeWithKitchenFormat(r.Start), parseTimeWithKitchenFormat(r.End)
		klog.V(5).Infof("start time paresed as %v, end time %v", s.Format(time.Kitchen), e.Format(time.Kitchen))

		if s.Before(e) {
			h = append(h, r)
		} else {
			r.Start, r.End = r.End, r.Start
			h = append(h, r)
		}
	}

	h = MergeHourRanges(h)

	return h
}

func validateWeekDaysSlice(wds []string) (runDays, runDays) {
	vwds := runDays{}

	weekdayMap := map[string]int{
		"sunday":    0,
		"monday":    1,
		"tuesday":   2,
		"wednesday": 3,
		"thursday":  4,
		"friday":    5,
		"saturday":  6,
	}

	found := make(map[string]bool)

	for _, wd := range wds {
		wd = strings.ToLower(wd)

		if v, ok := weekdayMap[wd]; ok {
			if _, ok := found[wd]; !ok {
				vwds = append(vwds, time.Weekday(v))
				found[wd] = true
			}
		}
	}

	rwds := make(runDays, 0)

	for k, v := range weekdayMap {
		if _, ok := found[k]; !ok {
			rwds = append(rwds, time.Weekday(v))
		}
	}

	return vwds, rwds
}

func parseTimeWithKitchenFormat(tstr string) time.Time {
	t, err := time.Parse(time.Kitchen, tstr)
	if err != nil {
		curTime := time.Now().Local()
		klog.Errorf("Error: %v, while parsing time string %v with the time.Kitchen format, will use the current time %v instead", err, tstr, curTime.String())

		return curTime
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
// if current time is smaller than the lastSlot time point, nextTime will be a duration till next time
//slot start point or a 0(if current time is within a time window)
func GenerateNextPoint(vhours RunHourRanges, rdays runDays, uniTime time.Time) time.Duration {
	if len(vhours) == 0 {
		timeByHour := parseTimeWithKitchenFormat(uniTime.Format(time.Kitchen))

		if len(rdays) == 0 {
			return rdays.DurationToNextRunableWeekday(uniTime)
		}

		return MIDNIGHT.Sub(timeByHour) + rdays.DurationToNextRunableWeekday(uniTime)
	}

	slots := sortRangeByStartTime(vhours)
	timeByHour := parseTimeWithKitchenFormat(uniTime.Format(time.Kitchen))
	// t is greater than todays window
	// eg t is 11pm
	// slots [1, 3 pm]
	lastSlot := parseTimeWithKitchenFormat(slots[len(slots)-1].End)

	if lastSlot.Before(timeByHour) {
		nxtStart := parseTimeWithKitchenFormat(slots[0].Start)
		dayOffsets := rdays.DurationToNextRunableWeekday(uniTime)

		// Hour difference is from curret day to midnight, then midnight to slot[0]
		// Current time to the next day's midnight
		return MIDNIGHT.Sub(timeByHour) + nxtStart.Sub(CURDAY) + dayOffsets
	}

	// t is at time range
	for _, slot := range slots {
		slotStart := parseTimeWithKitchenFormat(slot.Start)

		var slotEnd time.Time
		if slot.End == "12:00AM" {
			slotEnd = MIDNIGHT
		} else {
			slotEnd = parseTimeWithKitchenFormat(slot.End)
		}

		if timeByHour.Sub(slotStart) < 0 {
			return slotStart.Sub(timeByHour)
		} else if timeByHour.Sub(slotStart) > 0 && timeByHour.Sub(slotEnd) < 0 {
			return time.Duration(0)
		}
	}

	return time.Duration(-1)
}

func MaxHour(a, b string) string {
	if parseTimeWithKitchenFormat(a).Before(parseTimeWithKitchenFormat(b)) {
		return b
	}

	return a
}

func MergeHourRanges(in RunHourRanges) RunHourRanges {
	if len(in) < 2 {
		return in
	}

	out := make(RunHourRanges, 0)
	out = append(out, in[0])

	for i := 1; i < len(in); i++ {
		l := out[len(out)-1]
		r := in[i]

		e, s := parseTimeWithKitchenFormat(l.End), parseTimeWithKitchenFormat(r.Start)
		if e.Sub(s) >= 0 {
			m := MaxHour(l.End, r.End)
			out[len(out)-1].End = m
		} else {
			out = append(out, in[i])
		}

		l = out[len(out)-1]
	}

	return out
}

func ReverseRange(in RunHourRanges) RunHourRanges {
	if len(in) == 0 {
		return in
	}

	out := make(RunHourRanges, 0)

	tmp := appv1alpha1.HourRange{}
	tmp.Start = CURDAY.Format(time.Kitchen)
	tmp.End = in[0].Start
	out = append(out, tmp)

	if len(in) == 1 {
		tmp.Start = in[0].End
		tmp.End = MIDNIGHT.Format(time.Kitchen)
		out = append(out, tmp)

		return out
	}

	for i := 0; i < len(in)-1; i++ {
		tmp.Start = in[i].End
		tmp.End = in[i+1].Start
		out = append(out, tmp)
	}

	tmp.Start = in[len(in)-1].End
	tmp.End = MIDNIGHT.Format(time.Kitchen)
	out = append(out, tmp)

	return out
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
