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
	"reflect"
	"testing"
	"time"

	appv1alpha1 "github.com/open-cluster-management/multicloud-operators-subscription/pkg/apis/apps/v1"
)

func TestTimeWindowDurationTillNextWindow(t *testing.T) {
	testCases := []struct {
		desc    string
		curTime string
		windows *appv1alpha1.TimeWindow
		want    time.Duration
	}{
		{
			desc:    "default form of timewindow on UI-run all the time",
			curTime: "Sun Nov  3 10:40:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Location:   "America/Toronto",
			},
			want: 0,
		},
		{
			desc:    "nil timewindow",
			curTime: "Sun Nov  3 10:40:00 UTC 2019",
			want:    0,
		},
		{
			desc:    "empty timewindow",
			curTime: "Sun Nov  3 10:40:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{},
			want:    0,
		},
		{
			desc:    "run on current time without DaysofWeek",
			curTime: "Sun Nov  3 09:00:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "8:30PM"},
				},
				Location: "",
			},
			want: time.Hour*1 + time.Minute*30,
		},
		{
			desc:    "run on certain days-current time without hour ranges",
			curTime: "Sun Nov  3 09:00:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Daysofweek: []string{"monday", "friday"},
				Location:   "",
			},
			want: time.Hour * 15,
		},
		{
			desc:    "run on certain days with location offset",
			curTime: "Sun Nov  3 09:40:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "8:30PM"},
				},
				Daysofweek: []string{"Sunday", "monday", "friday"},
				Location:   "America/Toronto",
			},
			want: time.Minute*50 + time.Hour*5,
		},
		{
			desc: "block certain time return next active time",
			//this is sunday
			curTime: "Sun Nov  3 09:30:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "block",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "8:30PM"},
				},
				Daysofweek: []string{"Sunday", "monday", "friday"},
				Location:   "",
			},
			//next most recent time will be next tuesday 12:00AM, 24-9.40 + 24 = 14.20+24 = 38.30
			want: time.Minute*30 + time.Hour*14 + time.Hour*24,
		},
		{
			desc: "run only on Monday",
			//this is sunday
			curTime: "Sun Nov  3 09:00:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours:      []appv1alpha1.HourRange{},
				Daysofweek: []string{"Monday"},
				Location:   "",
			},
			want: time.Hour * 15,
		},
		{
			desc:    "currently blocked and return next active time",
			curTime: "Mon Sep  28 15:06:00 UTC 2020", // 11:06AM Toronto time
			windows: &appv1alpha1.TimeWindow{
				WindowType: "block",
				Hours: []appv1alpha1.HourRange{
					{Start: "08:18AM", End: "09:09PM"},
				},
				Location:   "America/Toronto",
				Daysofweek: []string{"Sunday", "Monday"},
			},
			// Currently at 11:06AM blocked. Should become active at 09:09PM, 10h3m0s
			want: time.Hour*10 + time.Minute*3,
		},
		{
			desc:    "currently not blocked",
			curTime: "Mon Sep  28 15:06:00 UTC 2020", // 11:06AM Toronto time
			windows: &appv1alpha1.TimeWindow{
				WindowType: "block",
				Hours: []appv1alpha1.HourRange{
					{Start: "11:07AM", End: "09:09PM"},
				},
				Location:   "America/Toronto",
				Daysofweek: []string{"Sunday", "Monday"},
			},
			// Currently at 11:06AM, not blocked.
			want: 0,
		},
	}

	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			c, _ := time.Parse(time.UnixDate, tC.curTime)
			got := NextStartPoint(tC.windows, c)

			if got != tC.want {
				t.Errorf("wanted time.Duration %v, got %v", tC.want, got)
			}
		})
	}
}

func TestValidateHours(t *testing.T) {
	f := func(ts, l string) time.Time {
		t, _ := time.ParseInLocation(time.Kitchen, ts, getLoc(l))
		return t
	}

	loc := "America/Toronto"

	testCases := []struct {
		desc     string
		rg       []appv1alpha1.HourRange
		location string
		wanted   []hourRangesInTime
	}{
		{
			desc: "time range without location",
			rg: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "11:30AM"},
				{Start: "12:30PM", End: "1:30PM"},
			},
			wanted: []hourRangesInTime{
				{start: f("10:30AM", ""), end: f("11:30AM", "")},
				{start: f("12:30PM", ""), end: f("1:30PM", "")},
			},
		},
		{
			desc: "time overlapped with location",
			rg: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "11:30AM"},
				{Start: "11:10AM", End: "1:30PM"},
			},
			location: loc,
			wanted: []hourRangesInTime{
				{start: f("10:30AM", loc), end: f("1:30PM", loc)},
			},
		},
		{
			desc: "time overlapped with location",
			rg: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "11:30AM"},
				{Start: "11:30AM", End: "1:30PM"},
			},
			location: loc,

			wanted: []hourRangesInTime{
				{start: f("10:30AM", loc), end: f("1:30PM", loc)},
			},
		},
		{
			desc: "multi-timewindow with location",
			rg: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "11:30AM"},
				{Start: "11:40AM", End: "5:30PM"},
				{Start: "12:40AM", End: "4:30PM"},
				{Start: "6:40PM", End: "6:30PM"},
			},
			location: loc,
			wanted: []hourRangesInTime{
				{start: f("12:40AM", loc), end: f("5:30PM", loc)},
				{start: f("6:30PM", loc), end: f("6:40PM", loc)},
			},
		},
	}

	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := validateHourRange(tC.rg, getLoc(tC.location))
			assertHourRangesInTime(t, got, tC.wanted)
		})
	}
}

func assertHourRangesInTime(t *testing.T, got, wanted []hourRangesInTime) {
	if len(got) != len(wanted) {
		t.Fatalf("validateHourRange length is wrong, got %v, wanted %v", len(got), len(wanted))
	}

	for i := 0; i < len(got); i++ {
		if got[i].start.Equal(wanted[i].start) == false {
			t.Errorf("item idx %v got %v, wanted %v", i, got[i].start.String(), wanted[i].start.String())
		}

		if got[i].end.Equal(wanted[i].end) == false {
			t.Errorf("item idx %v got %v, wanted %v", i, got[i].end.String(), wanted[i].end.String())
		}
	}
}

func TestReverseRange(t *testing.T) {
	loc := "America/Toronto"
	f := func(ts, l string) time.Time {
		t, _ := time.ParseInLocation(time.Kitchen, ts, getLoc(l))
		return t
	}

	lastMidnight := parseTimeWithKitchenFormat(MIDNIGHT, getLoc(loc))
	nextMidngith := lastMidnight.Add(time.Hour * 24)

	testCases := []struct {
		desc     string
		location string
		rg       []hourRangesInTime
		wanted   []hourRangesInTime
	}{
		{
			desc:     "",
			location: loc,
			rg: []hourRangesInTime{
				{start: f("10:30AM", loc), end: f("11:30AM", loc)},
				{start: f("11:40AM", loc), end: f("4:30PM", loc)},
			},
			wanted: []hourRangesInTime{
				{start: f("12:00AM", loc), end: f("10:30AM", loc)},
				{start: f("11:30AM", loc), end: f("11:40AM", loc)},
				{start: f("4:30PM", loc), end: nextMidngith},
			},
		},

		{
			desc:     "",
			location: loc,
			rg: []hourRangesInTime{
				{start: f("10:30AM", loc), end: f("11:30AM", loc)},
			},
			wanted: []hourRangesInTime{
				{start: f("12:00AM", loc), end: f("10:30AM", loc)},
				{start: f("11:30AM", loc), end: nextMidngith},
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := reverseRange(tC.rg, getLoc(tC.location))
			if !reflect.DeepEqual(got, tC.wanted) {
				t.Errorf("got %v, want %v", got, tC.wanted)
			}
		})
	}
}

func TestNextWeekdayToRun(t *testing.T) {
	testCases := []struct {
		desc   string
		rd     runDays
		t      string
		wanted time.Duration
	}{
		{
			desc: "",
			rd:   []time.Weekday{3, 2, 1, 5},
			// 6
			t:      "Sat Nov  2 14:00:00 UTC 2019",
			wanted: time.Hour * 1 * 24,
		},
		{
			desc: "",
			rd:   []time.Weekday{3, 2, 1, 5, 0},
			// 6
			t:      "Sun Nov  3 14:00:00 UTC 2019",
			wanted: time.Hour * 0 * 24,
		},
		{
			desc: "",
			rd:   []time.Weekday{},
			// 6
			t:      "Sat Nov  2 14:00:00 UTC 2019",
			wanted: time.Hour * 0,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			tt, _ := time.Parse(time.UnixDate, tC.t)
			got := tC.rd.durationToNextRunableWeekday(tt.Weekday())
			if got != tC.wanted {
				t.Errorf("wanted %v, however got %v", tC.wanted, got)
			}
		})
	}
}

func TestParseTimeWithKicthenFormat(t *testing.T) {
	testCases := []struct {
		desc   string
		tstr   string
		loc    string
		wanted string
	}{
		{
			desc:   "legal format parsed to UTC",
			tstr:   "10:30AM",
			loc:    "America/Toronto",
			wanted: "Sat Jan  1 10:30:00 UTC 0000",
		},
		{
			desc:   "illegal format",
			tstr:   "10:30am",
			wanted: time.Now().UTC().Format(time.UnixDate),
		},
	}

	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			tt, _ := time.Parse(time.UnixDate, tC.wanted)
			pTime := parseTimeWithKitchenFormat(tC.tstr, getLoc(tC.loc))
			p, g := pTime.Format(time.Kitchen), tt.Format(time.Kitchen)
			if p != g {
				t.Errorf("parsed time %v, wanted %v", p, g)
			}
		})
	}
}

func getTime(t string) time.Time {
	tt, _ := time.Parse(time.UnixDate, t)
	return tt
}

func TestNextStatusReconcile(t *testing.T) {
	var tests = []struct {
		name     string
		expected time.Duration
		giventw  *appv1alpha1.TimeWindow
		giventm  time.Time
	}{
		{
			name:     "empty time window",
			expected: time.Duration(0),
			giventw:  &appv1alpha1.TimeWindow{},
			giventm:  getTime("Sat Jan  1 10:30:00 UTC 0000"),
		},
		{
			name:     "empty time window",
			expected: time.Duration(0),
			giventm:  getTime("Sat Jan  1 10:30:00 UTC 0000"),
		},
		{
			name:     "empty time window",
			expected: time.Duration(0),
			giventw: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Location:   "UTC",
				Hours:      []appv1alpha1.HourRange{},
			},
			giventm: getTime("Sat Jan  1 10:30:00 UTC 0000"),
		},
		{
			name:     "time window only have hours",
			expected: time.Minute * 31,
			giventw: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Location:   "UTC",
				Hours: []appv1alpha1.HourRange{
					{Start: "11:00AM", End: "12:00PM"},
				},
			},
			giventm: getTime("Sat Jan  1 10:30:00 UTC 0000"),
		},
		{
			name:     "time window only have daysofweek",
			expected: time.Hour*48 + time.Minute*1,
			giventw: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Location:   "UTC",
				Daysofweek: []string{"monday", "friday"},
				Hours:      []appv1alpha1.HourRange{},
			},
			giventm: getTime("Sat Jan  1 00:00:00 UTC 0000"),
		},
		{
			name:     "time window with hour range and daysofweek-happy path outside timewindow",
			expected: time.Hour*59 + time.Minute*1,
			giventw: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Location:   "UTC",
				Daysofweek: []string{"monday", "friday"},
				Hours: []appv1alpha1.HourRange{
					{Start: "11:00AM", End: "12:00PM"},
				},
			},
			giventm: getTime("Sat Jan  1 00:00:00 UTC 0000"),
		},
		{
			name:     "time window have hour range and daysofweek-time on edge",
			expected: time.Minute * 1,
			giventw: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Location:   "UTC",
				Daysofweek: []string{"monday", "friday", "saturday"},
				Hours: []appv1alpha1.HourRange{
					{Start: "11:00AM", End: "12:00PM"},
				},
			},
			giventm: getTime("Sat Jan  1 11:00:00 UTC 0000"),
		},
		{
			name:     "time window with hour range and daysofweek-happy path within timewindow",
			expected: time.Minute * 31,
			giventw: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Location:   "UTC",
				Daysofweek: []string{"monday", "friday", "saturday"},
				Hours: []appv1alpha1.HourRange{
					{Start: "11:00AM", End: "12:00PM"},
				},
			},
			giventm: getTime("Sat Jan  1 11:30:00 UTC 0000"),
		},
		{
			name: "block time window with hour range and daysofweek-happy path outside timewindow",
			// Currently not blocked. Should be blocked in 2 days and 11 hours = 59 hours
			expected: time.Hour*59 + time.Minute*1,
			giventw: &appv1alpha1.TimeWindow{
				WindowType: "block",
				Location:   "UTC",
				Daysofweek: []string{"monday", "friday"},
				Hours: []appv1alpha1.HourRange{
					{Start: "11:00AM", End: "12:00PM"},
				},
			},
			giventm: getTime("Sat Jan  1 00:00:00 UTC 0000"),
		},
		{
			name:     "block time window have hour range and daysofweek-time on edge",
			expected: time.Minute * 1,
			giventw: &appv1alpha1.TimeWindow{
				WindowType: "block",
				Location:   "UTC",
				Daysofweek: []string{"monday", "friday", "saturday"},
				Hours: []appv1alpha1.HourRange{
					{Start: "11:00AM", End: "12:00PM"},
				},
			},
			giventm: getTime("Sat Jan  1 11:00:00 UTC 0000"),
		},
		{
			name:     "block time window with hour range and daysofweek-happy path within timewindow",
			expected: time.Minute * 31,
			giventw: &appv1alpha1.TimeWindow{
				WindowType: "block",
				Location:   "UTC",
				Daysofweek: []string{"monday", "friday", "saturday"},
				Hours: []appv1alpha1.HourRange{
					{Start: "11:00AM", End: "12:00PM"},
				},
			},
			giventm: getTime("Sat Jan  1 11:30:00 UTC 0000"),
		},
		{
			name:    "currently blocked and return next active time",
			giventm: getTime("Mon Sep  28 15:06:00 UTC 2020"), // 11:06AM Toronto time
			giventw: &appv1alpha1.TimeWindow{
				WindowType: "block",
				Hours: []appv1alpha1.HourRange{
					{Start: "08:18AM", End: "09:09PM"},
				},
				Location:   "America/Toronto",
				Daysofweek: []string{"Sunday", "Monday"},
			},
			// Currently at 11:06AM blocked. Should become active at 09:09PM, so next status reconcile in 10h4m0s
			expected: time.Hour*10 + time.Minute*4,
		},
		{
			name:    "currently not blocked and return next blocked time",
			giventm: getTime("Mon Sep  28 11:06:00 UTC 2020"),
			giventw: &appv1alpha1.TimeWindow{
				WindowType: "block",
				Hours: []appv1alpha1.HourRange{
					{Start: "11:09AM", End: "09:09PM"},
				},
				Location:   "UTC",
				Daysofweek: []string{"Sunday", "Monday"},
			},
			// Currently at 11:06AM, not blocked. Should be blocked in 3 minutes so next status reconcile should be 3 + 1 minutes.
			expected: time.Minute * 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := NextStatusReconcile(tt.giventw, tt.giventm)
			if actual != tt.expected {
				t.Errorf("(%#v): expected %v, actual %v", tt.giventw, tt.expected, actual)
			}
		})
	}
}
