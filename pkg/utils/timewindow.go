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

	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

const (
	//MIDNIGHT define the midnight format
	MIDNIGHT        = "12:00AM"
	originYear      = 0
	originMonth     = 1
	originDay       = 1
	debuglevel      = klog.Level(5)
	totaldaysofweek = 7
)

// IsInWindow returns true if the give time is within a timewindow
func IsInWindow(tw *appv1alpha1.TimeWindow, t time.Time) bool {
	if tw == nil {
		return true
	}

	return NextStartPoint(tw, t) == 0
}

// NextStatusReconcile generate a duartion for the reconcile to requeue after
func NextStatusReconcile(tw *appv1alpha1.TimeWindow, t time.Time) time.Duration {
	if tw == nil {
		return time.Duration(0)
	}

	uniCurTime := UnifyTimeZone(tw, t)

	if len(tw.Daysofweek) == 0 && len(tw.Hours) == 0 {
		return time.Duration(0)
	}

	if IsInWindow(tw, uniCurTime) {
		vHr := validateHourRange(tw.Hours, getLoc(tw.Location))
		vdays, _ := validateDaysofweekSlice(tw.Daysofweek)
		rvevHr := reverseRange(vHr, getLoc(tw.Location))

		// If currently not blocked but the time window type is `blocked`, we need to get the next time
		// it should be blocked.
		if tw.WindowType != "" && (strings.EqualFold(tw.WindowType, "block") || strings.EqualFold(tw.WindowType, "blocked")) {
			return generateNextPoint(vHr, vdays, uniCurTime, false) + 1*time.Minute
		}

		// The window type is active so we need to get the next time it should be blocked.
		return generateNextPoint(rvevHr, vdays, uniCurTime, false) + 1*time.Minute
	}

	return NextStartPoint(tw, uniCurTime) + 1*time.Minute
}

//NextStartPoint will map the container's time to the location time specified by user
// then it will handle the window type as will the hour ange and daysofweek
// for hour range and daysofweek, it will handle as the following
// if hour range is empty and weekday is empty then retrun 0
// if hour range is empty and weekday is not then return nextday durtion(here the window type will be considered again)
func NextStartPoint(tw *appv1alpha1.TimeWindow, t time.Time) time.Duration {
	if tw == nil {
		return time.Duration(0)
	}

	// convert current time to the location time defined within the timewindow
	uniCurTime := UnifyTimeZone(tw, t)

	klog.V(debuglevel).Infof("Time window checking at %v", uniCurTime.String())

	// valid hour ranges, meaning the each hour range's start time is earlier than the end time
	// also there's no overlap between 2 ranges
	vHr := validateHourRange(tw.Hours, getLoc(tw.Location))

	rDays, rveDays := validateDaysofweekSlice(tw.Daysofweek)

	if tw.WindowType != "" && (strings.EqualFold(tw.WindowType, "block") || strings.EqualFold(tw.WindowType, "blocked")) {
		// reverse slots, the time slots are applicable only for blocked days of the week.
		// If today is not one of the days specified, just return 0
		rvevHr := reverseRange(vHr, getLoc(tw.Location))
		return generateNextPoint(rvevHr, rveDays, uniCurTime, true)
	}

	// generate the duration for t
	return generateNextPoint(vHr, rDays, uniCurTime, false)
}

// UnifyTimeZone convert a given time to the timewindow time zone, if the time window doesn't sepcifiy a
// time zone, then the running machine's time zone will be used
func UnifyTimeZone(tw *appv1alpha1.TimeWindow, t time.Time) time.Time {
	lptr := getLoc(tw.Location)
	return t.In(lptr)
}

func getLoc(loc string) *time.Location {
	l, err := time.LoadLocation(loc)
	if err != nil {
		klog.Errorf("Error %v while parsing the location string \"%v\", will UTC as location reference %v ", err, loc, l)
	}

	return l
}

type hourRangesInTime struct {
	start time.Time
	end   time.Time
}

// return will be sorted and marged hour ranges
func validateHourRange(rg []appv1alpha1.HourRange, loc *time.Location) []hourRangesInTime {
	if len(rg) == 0 {
		return []hourRangesInTime{}
	}

	h := make([]hourRangesInTime, 0)

	for _, r := range rg {
		s, e := parseTimeWithKitchenFormat(r.Start, loc), parseTimeWithKitchenFormat(r.End, loc)
		klog.V(debuglevel).Infof("start time paresed as %v, end time %v", s.Format(time.Kitchen), e.Format(time.Kitchen))

		if s.Before(e) {
			h = append(h, hourRangesInTime{start: s, end: e})
		} else {
			h = append(h, hourRangesInTime{start: e, end: s})
		}
	}

	return mergeHourRanges(h)
}

func validateDaysofweekSlice(wds []string) (runDays, runDays) {
	if len(wds) == 0 {
		return runDays{}, runDays{}
	}

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

func parseTimeWithKitchenFormat(tstr string, loc *time.Location) time.Time {
	t, err := time.ParseInLocation(time.Kitchen, tstr, loc)
	if err != nil {
		curTime := time.Now().UTC()
		klog.Errorf("can't paser the time %v, err: %v, will use the current time %v instead", tstr, err, curTime.String())

		return curTime
	}

	return t
}

// next time will be:
// if current time is bigger than the last time point of the window, nextTime will be daysofweek offset + the hour offset
// if current time is smaller than the lastSlot time point, nextTime will be a duration till next time
//slot start point or a 0(if current time is within a time window)
func generateNextPoint(slots []hourRangesInTime, rdays runDays, uniCurTime time.Time, blocked bool) time.Duration {
	if len(slots) == 0 && len(rdays) == 0 {
		return time.Duration(0)
	}

	if blocked {
		// rdays is the list of week days that are not blocked. If today is one of these days, just return 0
		// to indicate that it is in active window.
		for _, runDay := range rdays {
			if runDay == uniCurTime.Weekday() {
				return time.Duration(0)
			}
		}
	}

	if len(slots) == 0 && len(rdays) != 0 {
		if rdays.isCurDayInDaysOfWeek(uniCurTime.Weekday()) {
			klog.Infof("Today is in valid Daysofweek time window. Today: %v, valid Daysofweek: %v\n", uniCurTime.Weekday(), rdays)
			return time.Duration(0)
		}

		return timeLeftTillNextMidNight(uniCurTime) + rdays.durationToNextRunableWeekday(uniCurTime.Weekday())
	}

	if len(slots) != 0 && len(rdays) == 0 {
		return tillNextSlot(slots, uniCurTime)
	}

	if rdays.durationToNextRunableWeekday(uniCurTime.Weekday()) == 0 {
		return tillNextSlot(slots, uniCurTime)
	}

	return timeLeftTillNextMidNight(uniCurTime) + tillNextSlot(slots, uniCurTime) + rdays.durationToNextRunableWeekday(uniCurTime.Weekday())
}

func timeLeftTillNextMidNight(cur time.Time) time.Duration {
	lastMidnight := parseTimeWithKitchenFormat(MIDNIGHT, cur.Location())
	nextMidngiht := lastMidnight.Add(time.Hour * 24)

	gt := time.Date(originYear, originMonth, originDay, cur.Hour(), cur.Minute(), 0, 0, cur.Location())

	return nextMidngiht.Sub(gt)
}

func tillNextSlot(slots []hourRangesInTime, cur time.Time) time.Duration {
	lastMidnight := parseTimeWithKitchenFormat(MIDNIGHT, cur.Location())
	gt := time.Date(originYear, originMonth, originDay, cur.Hour(), cur.Minute(), 0, 0, cur.Location())
	// t is greater than todays window
	// eg t is 11pm
	// slots [1, 3 pm]
	lastSlot := slots[len(slots)-1].end

	if lastSlot.Before(gt) {
		nxtStart := slots[0].start
		// Hour difference is from curret day to midnight, then midnight to slot[0]
		// Current time to the next day's midnight
		return timeLeftTillNextMidNight(gt) + nxtStart.Sub(lastMidnight)
	}

	for _, slot := range slots {
		st, ed := slot.start, slot.end

		if gt.Sub(st) <= 0 {
			return st.Sub(gt)
		} else if gt.Sub(st) > 0 && gt.Sub(ed) <= 0 {
			return time.Duration(0)
		}
	}

	return time.Duration(0)
}

func maxHour(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}

	return a
}

func mergeHourRanges(in []hourRangesInTime) []hourRangesInTime {
	if len(in) < 2 {
		return in
	}

	sort.Slice(in, func(i int, j int) bool { return in[i].start.Before(in[j].start) })

	out := make([]hourRangesInTime, 0)
	out = append(out, in[0])

	l := 0

	for i := 1; i < len(in); i++ {
		ed := out[l].end
		s := in[i].start

		if s.Before(ed) || s.Equal(ed) {
			m := maxHour(ed, in[i].end)
			out[l].end = m
		} else {
			out = append(out, in[i])
			l++
		}
	}

	return out
}

func isThereGap(st, ed time.Time) bool {
	return ed.Sub(st) != 0
}

func reverseRange(in []hourRangesInTime, loc *time.Location) []hourRangesInTime {
	if len(in) == 0 {
		return in
	}

	out := make([]hourRangesInTime, 0)

	lastMidnight := parseTimeWithKitchenFormat(MIDNIGHT, loc)

	sp := lastMidnight

	for _, slot := range in {
		if isThereGap(sp, slot.start) {
			out = append(out, hourRangesInTime{start: sp, end: slot.start})
		}

		sp = slot.end
	}

	nextMidngith := lastMidnight.Add(time.Hour * 24)
	if isThereGap(sp, nextMidngith) {
		out = append(out, hourRangesInTime{start: sp, end: nextMidngith})
	}

	return out
}

// thie field is actually int
type runDays []time.Weekday

// type runDays []time.Weekday
func (r runDays) durationToNextRunableWeekday(curWeekday time.Weekday) time.Duration {
	// if daysofweek is sorted, we want the next day with is greater than the t.Weekday
	// the daysofweek is loop such as [3, 4, 5], if t==6, we should return 3 aka 4 days
	// if t == 2 then we should return 1
	if len(r) == 0 {
		// this mean you will wait for less than a day.
		return time.Duration(0)
	}

	sort.Slice(r, func(i, j int) bool { return r[i] < r[j] })

	var days int

	//if the current weekday is later than the latest weekday in the time window,
	//then we find the days left till first runable weekday
	if curWeekday > r[len(r)-1] {
		daysLeftOfThisWeek := totaldaysofweek - int(curWeekday)
		mostRecentWeekday := int(r[0])
		days = daysLeftOfThisWeek + mostRecentWeekday
	} else {
		for _, d := range r {
			if curWeekday == d {
				return time.Duration(0)
			}

			if curWeekday < d {
				days = int(d - curWeekday)
				break
			}
		}
	}

	return time.Duration(days-1) * time.Hour * 24
}

func (r runDays) isCurDayInDaysOfWeek(curWeekday time.Weekday) bool {
	for _, d := range r {
		if curWeekday == d {
			return true
		}
	}

	return false
}
