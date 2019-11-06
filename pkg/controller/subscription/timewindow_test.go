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
	"testing"
	"time"

	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
)

func TestTimeWindow(t *testing.T) {
	TZ, _ := time.LoadLocation("Local")
	testCases := []struct {
		desc    string
		curTime time.Time
		windows *appv1alpha1.TimeWindow
		want    time.Duration
	}{

		{
			desc:    "the time is within the time windows",
			curTime: time.Date(2019, 11, 3, 10, 40, 00, 00, TZ),
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "8:30PM"},
				},
				Weekdays: []time.Weekday{0, 1, 5},
				Location: "",
			},
			want: time.Duration(0),
		},

		{
			desc:    "the time is within the time windows",
			curTime: time.Date(2019, 11, 3, 9, 40, 00, 00, TZ),
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "8:30PM"},
				},
				Weekdays: []time.Weekday{0, 1, 5},
				Location: "",
			},
			want: time.Duration(time.Minute * 50),
		},

		{
			desc: "the time out of the range, need to offset by day",
			// weekday == 0
			curTime: time.Date(2019, 11, 3, 14, 25, 00, 00, TZ),
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "1:30PM"},
				},
				Weekdays: []time.Weekday{0, 1, 5},
				Location: "",
			},
			want: time.Duration(20*time.Hour) + time.Duration(5*time.Minute),
		},

		{
			desc: "the time out of the range, need to offset by day",
			// weekday == 6
			curTime: time.Date(2019, 11, 6, 14, 21, 00, 00, TZ),
			windows: &appv1alpha1.TimeWindow{
				WindowType: "active",
				Hours: []appv1alpha1.HourRange{
					{Start: "10:30AM", End: "11:30AM"},
					{Start: "12:30PM", End: "1:30PM"},
				},
				Weekdays: []time.Weekday{0, 1, 5},
				Location: "",
			},
			want: time.Duration(44*time.Hour) + time.Duration(9*time.Minute),
		},
	}

	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := NextStartPoint(tC.windows, tC.curTime)

			if got != tC.want {
				t.Errorf("wanted time.Duration %v, got %v", tC.want, got)
			}
		})
	}
}

func TestNextWeekdayToRun(t *testing.T) {
	testCases := []struct {
		desc   string
		rd     runDays
		t      time.Time
		wanted time.Duration
	}{
		{
			desc: "",
			rd:   []time.Weekday{3, 2, 1, 5},
			// 6
			t:      time.Date(2019, 11, 2, 14, 00, 00, 00, time.UTC),
			wanted: time.Hour * 1 * 24,
		},
		{
			desc: "",
			rd:   []time.Weekday{3, 2, 1, 5, 0},
			// 6
			t:      time.Date(2019, 11, 3, 14, 00, 00, 00, time.UTC),
			wanted: time.Hour * 0 * 24,
		},
		{
			desc: "",
			rd:   []time.Weekday{},
			// 6
			t:      time.Date(2019, 11, 2, 14, 00, 00, 00, time.UTC),
			wanted: time.Hour * 0,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := tC.rd.NextWeekdayToRun(tC.t)
			if got != tC.wanted {
				t.Errorf("wanted %v, however got %v", tC.wanted, got)
			}
		})
	}
}

// func TestParseTimeWithKicFormat(t *testing.T) {
// 	testCases := []struct {
// 		desc   string
// 		tstr   string
// 		wanted time.Time
// 	}{
// 		{
// 			desc:   "legal format parsed to UTC",
// 			tstr:   "10:30AM",
// 			wanted: time.Now(),
// 		},
// 		{
// 			desc:   "illegal format",
// 			tstr:   "10:30am",
// 			wanted: time.Now(),
// 		},
// 	}

// 	for _, tC := range testCases {
// 		t.Run(tC.desc, func(t *testing.T) {
// 			pTime := parseTimeWithKitchenFormat(tC.tstr)
// 			t.Errorf("time string %v is parsed to %v ", tC.tstr, pTime.String())
// 			if pTime == tC.wanted {
// 				t.Errorf("time string %v is parsed to %v ", tC.tstr, pTime.String())
// 			}

// 		})
// 	}
// }

// func TestRangeSort(t *testing.T) {
// 	testCases := []struct {
// 		desc     string
// 		before   bool
// 		hR       RunHourRanges
// 		isSorted bool
// 	}{
// 		{
// 			desc: "testing RangeHourRanges",
// 			hR: RunHourRanges{
// 				HourRange{Start: "10:30AM", End: "11:30AM"},
// 				HourRange{Start: "12:30PM", End: "1:30PM"},
// 				HourRange{Start: "9:30AM", End: "9:45AM"},
// 			},
// 			isSorted: true,
// 		},
// 	}
// 	for _, tC := range testCases {
// 		t.Run(tC.desc, func(t *testing.T) {
// 			sort.Sort(tC.hR)
// 			got := sort.IsSorted(tC.hR)
// 			if got != tC.isSorted {
// 				t.Errorf("sorted hour ranges %v ", tC.hR)
// 			}
// 		})
// 	}
// }
