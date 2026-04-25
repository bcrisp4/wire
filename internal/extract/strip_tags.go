// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
//
// Vendored from miniflux/v2 internal/reader/sanitizer/strip_tags.go (Apache-2.0).
// Source: https://github.com/miniflux/v2/blob/main/internal/reader/sanitizer/strip_tags.go
//
// Wire-local modifications: package renamed from `sanitizer` to `extract`.

package extract

import (
	"io"
	"strings"

	"golang.org/x/net/html"
)

// StripTags removes all HTML/XML tags from the input string.
// This function must *only* be used for cosmetic purposes (e.g. counting
// words for reading time), not to prevent code injections like XSS.
func StripTags(input string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(input))
	var buffer strings.Builder

	for {
		if tokenizer.Next() == html.ErrorToken {
			err := tokenizer.Err()
			if err == io.EOF {
				return buffer.String()
			}
			return ""
		}

		token := tokenizer.Token()
		if token.Type == html.TextToken {
			buffer.WriteString(token.Data)
		}
	}
}
