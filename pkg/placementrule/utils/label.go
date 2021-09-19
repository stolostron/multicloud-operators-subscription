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
	"fmt"
	"unicode"
)

// ValidateK8sLabel returns a valid k8s label string by enforcing k8s label values rules as below
// 1. Must consist of alphanumeric characters, '-', '_' or '.'
//    No need to check this as the input string is the host name of the k8s api url
// 2. Must be no more than 63 characters
// 3. Must start and end with an alphanumeric character
func ValidateK8sLabel(s string) string {
	// must be no more than 63 characters
	s = fmt.Sprintf("%.63s", s)

	// look for the first alphanumeric byte from the start
	start := 0
	for ; start < len(s); start++ {
		c := s[start]
		if unicode.IsLetter(rune(c)) || unicode.IsNumber(rune(c)) {
			break
		}
	}

	// Now look for the first alphanumeric byte from the end
	stop := len(s)
	for ; stop > start; stop-- {
		c := s[stop-1]
		if unicode.IsLetter(rune(c)) || unicode.IsNumber(rune(c)) {
			break
		}
	}

	return s[start:stop]
}
