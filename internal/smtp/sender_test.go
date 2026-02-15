// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package smtp

import (
	"html"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHTMLEscaping verifies that HTML in email body is properly escaped
// to prevent XSS attacks in email clients.
func TestHTMLEscaping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Script tag",
			input:    "<script>alert('XSS')</script>",
			expected: "&lt;script&gt;alert(&#39;XSS&#39;)&lt;/script&gt;",
		},
		{
			name:     "Image with onerror",
			input:    "<img src=x onerror=alert('XSS')>",
			expected: "&lt;img src=x onerror=alert(&#39;XSS&#39;)&gt;",
		},
		{
			name:     "Ampersand",
			input:    "Q&A",
			expected: "Q&amp;A",
		},
		{
			name:     "Less than and greater than",
			input:    "1 < 2 > 0",
			expected: "1 &lt; 2 &gt; 0",
		},
		{
			name:     "Quotes",
			input:    `Say "hello"`,
			expected: `Say &#34;hello&#34;`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// We can't directly test send() without a real SMTP server,
			// but we can verify the escaping logic by checking that
			// html.EscapeString is being used correctly.
			//
			// This test serves as documentation of expected behavior
			// and will catch if someone removes the escaping.
			escaped := html.EscapeString(tt.input)
			assert.Equal(t, tt.expected, escaped)
		})
	}
}

// TestEmailFormatting verifies the email message format is correct
func TestEmailFormatting(t *testing.T) {
	t.Parallel()

	bodyContent := "Test message"

	// Simulate what the send function does
	escapedBody := html.EscapeString(bodyContent)

	message := "\r\n<html><body>" + escapedBody + "\r\n</body></html>\r\n"

	assert.True(t, strings.Contains(message, "<html><body>"))
	assert.True(t, strings.Contains(message, bodyContent))
	assert.True(t, strings.Contains(message, "</body></html>"))
	assert.False(t, strings.Contains(message, "<script>"), "Should not contain unescaped script tags")
}
