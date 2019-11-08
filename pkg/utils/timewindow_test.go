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

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
)

func TestTimeWindowDurationTillNextWindow(t *testing.T) {
	testCases := []struct {
		desc    string
		curTime string
		windows *appv1alpha1.TimeWindow
		want    time.Duration
	}{
		{
			desc:    "the time is within the time windows",
			curTime: "Sun Nov  3 10:40:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "8:30PM"},
				},
				Weekdays: []string{"Sunday", "monday", "friday"},
				Location: "",
			},
			want: 0,
		},
		{
			desc:    "the time is within the time windows",
			curTime: "Sun Nov  3 09:40:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "8:30PM"},
				},
				Weekdays: []string{"Sunday", "monday", "friday"},
				Location: "",
			},
			want: time.Minute * 50,
		},
		{
			desc:    "the time is within the time windows, with location",
			curTime: "Sun Nov  3 09:40:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "8:30PM"},
				},
				Weekdays: []string{"Sunday", "monday", "friday"},
				Location: "America/Toronto",
			},
			want: time.Minute*50 + time.Hour*5,
		},
		{
			desc: "the time out of the range, need to offset by day",
			// weekday == 6
			curTime: "Wed Nov  6 14:21:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "1:30PM"},
				},
				Weekdays: []string{"Sunday", "monday", "friday"},
				Location: "",
			},
			want: 44*time.Hour + 9*time.Minute,
		},
		{
			desc: "the time is within the time windows",
			//this is sunday
			curTime: "Sun Nov  3 09:40:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "block",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "8:30PM"},
				},
				Weekdays: []string{"Sunday", "monday", "friday"},
				Location: "",
			},
			//next most recent time will be next tuesday 12:00AM, 24-9.40 + 24 = 14.20+24 = 38.20
			want: time.Minute*20 + time.Hour*38,
		},
		{
			desc: "only weekdays is empty",
			//this is sunday
			curTime: "Sun Nov  3 09:30:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "8:30PM"},
				},
				Weekdays: []string{},
				Location: "",
			},
			//next most recent time will be next tuesday 12:00AM, 24-9.40 + 24 = 14.20+24 = 38.20
			want: time.Hour * 1,
		},
		{
			desc: "both empty hour and weekdays",
			//this is sunday
			curTime: "Sun Nov  3 09:30:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours:      []appv1alpha1.HourRange{},
				Weekdays:   []string{},
				Location:   "",
			},
			//next most recent time will be next tuesday 12:00AM, 24-9.40 + 24 = 14.20+24 = 38.20
			want: 0,
		},
		{
			desc: "empty hours only",
			//this is sunday
			curTime: "Sun Nov  3 09:00:00 UTC 2019",
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours:      []appv1alpha1.HourRange{},
				Weekdays:   []string{"Monday"},
				Location:   "",
			},
			//next most recent time will be next tuesday 12:00AM, 24-9.40 + 24 = 14.20+24 = 38.20
			want: time.Hour * 15,
		},
		// {
		// 	desc: "only weekdays is empty",
		// 	//this is sunday
		// 	curTime: "Thu Nov  7 14:00:00 EST 2019",
		// 	windows: &appv1alpha1.TimeWindow{
		// 		WindowType: "active",
		// 		Hours: []appv1alpha1.HourRange{
		// 			{Start: "10:30AM", End: "1:30PM"},
		// 		},
		// 		Weekdays: []string{},
		// 		Location: "America/Toronto",
		// 	},
		// 	//next most recent time will be next tuesday 12:00AM, 24-9.40 + 24 = 14.20+24 = 38.20
		// 	want: time.Hour*20 + time.Minute*30,
		// },
		// {
		// 	desc: "reversion order of incoming hours",
		// 	//this is sunday
		// 	curTime: "Thu Nov  7 14:00:00 EST 2019",
		// 	windows: &appv1alpha1.TimeWindow{
		// 		WindowType: "active",
		// 		Hours: []appv1alpha1.HourRange{
		// 			{Start: "1:30PM", End: "10:30AM"},
		// 		},
		// 		Weekdays: []string{},
		// 		Location: "America/Toronto",
		// 	},
		// 	//next most recent time will be next tuesday 12:00AM, 24-9.40 + 24 = 14.20+24 = 38.20
		// 	want: time.Hour*20 + time.Minute*30,
		// },
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

func TestMergeHourRanges(t *testing.T) {
	testCases := []struct {
		desc   string
		rg     RunHourRanges
		wanted RunHourRanges
	}{
		{
			desc: "",
			rg: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "11:30AM"},
				{Start: "12:30PM", End: "1:30PM"},
			},

			wanted: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "11:30AM"},
				{Start: "12:30PM", End: "1:30PM"},
			},
		},
		{
			desc: "",
			rg: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "11:30AM"},
				{Start: "11:10AM", End: "1:30PM"},
			},

			wanted: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "1:30PM"},
			},
		},
		{
			desc: "",
			rg: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "11:30AM"},
				{Start: "11:30AM", End: "1:30PM"},
			},

			wanted: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "1:30PM"},
			},
		},
		{
			desc: "",
			rg: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "11:30AM"},
				{Start: "11:40AM", End: "5:30PM"},
				{Start: "12:40AM", End: "4:30PM"},
			},
			wanted: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "11:30AM"},
				{Start: "11:40AM", End: "5:30PM"},
			},
		},
	}

	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := MergeHourRanges(tC.rg)
			if !isEqualRanges(got, tC.wanted) {
				t.Errorf("wanted %v got %v", tC.wanted, got)
			}
		})
	}
}

func isEqualRanges(a, b RunHourRanges) bool {
	if len(a) != len(b) {
		return false
	}

	for i := 0; i < len(a); i++ {
		if a[i].Start != b[i].Start {
			return false
		}

		if a[i].End != b[i].End {
			return false
		}
	}

	return true
}

func TestReverseRange(t *testing.T) {
	testCases := []struct {
		desc   string
		rg     RunHourRanges
		wanted RunHourRanges
	}{
		{
			desc: "",
			rg: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "11:30AM"},
				{Start: "11:40AM", End: "4:30PM"},
			},
			wanted: []appv1alpha1.HourRange{
				{Start: "12:00AM", End: "10:30AM"},
				{Start: "11:30AM", End: "11:40AM"},
				{Start: "4:30PM", End: "12:00AM"},
			},
		},

		{
			desc: "",
			rg: []appv1alpha1.HourRange{
				{Start: "10:30AM", End: "11:30AM"},
			},
			wanted: []appv1alpha1.HourRange{
				{Start: "12:00AM", End: "10:30AM"},
				{Start: "11:30AM", End: "12:00AM"},
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := ReverseRange(tC.rg)
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
			got := tC.rd.DurationToNextRunableWeekday(tt)
			if got != tC.wanted {
				t.Errorf("wanted %v, however got %v", tC.wanted, got)
			}
		})
	}
}

func TestParseTimeWithKicFormat(t *testing.T) {
	testCases := []struct {
		desc   string
		tstr   string
		wanted string
	}{
		{
			desc:   "legal format parsed to UTC",
			tstr:   "10:30AM",
			wanted: "Sat Jan  1 10:30:00 UTC 0000",
		},
		{
			desc:   "illegal format",
			tstr:   "10:30am",
			wanted: time.Now().Local().Format(time.UnixDate),
		},
	}

	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			tt, _ := time.Parse(time.UnixDate, tC.wanted)
			pTime := parseTimeWithKitchenFormat(tC.tstr)
			p, g := pTime.Format(time.Kitchen), tt.Format(time.Kitchen)
			if p != g {
				t.Errorf("parsed time %v, wanted %v", p, g)
			}
		})
	}
}
