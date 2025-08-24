// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

package http

import (
	"embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

func addFrontendWildcards(staticGroup *gin.RouterGroup, depth int) {
	staticGroup.GET("/", func(c *gin.Context) {
		file, err := FS.Open("frontend/dist/index.html")
		if err != nil {
			slog.Error("Failed to open file", "error", err)
			return
		}
		defer func() {
			err := file.Close()
			if err != nil {
				slog.Error("Failed to close file", "error", err)
			}
		}()
		fileContent, getErr := io.ReadAll(file)
		if getErr != nil {
			slog.Error("Failed to read file", "error", getErr)
		}
		c.Data(http.StatusOK, "text/html", fileContent)
	})
	wildcard := "/:wild"
	for i := 0; i < depth; i++ {
		// We need to make a string that contains /:wild for each depth
		// After the first depth, we need to add a number to the end of the wild
		// Example for depth 3: /:wild/:wild2/:wild3
		if i > 0 {
			wildcard += fmt.Sprintf("/:wild%d", i)
		}
		thisDepth := i
		staticGroup.GET(wildcard, func(c *gin.Context) {
			wildPath := "frontend/dist"
			// We need to get the wildcards and add them to the path
			// Example for depth 3: /:wild/:wild2/:wild3

			// Get the first wildcard
			wild, have := c.Params.Get("wild")
			if !have {
				slog.Error("Failed to get wildcard")
				return
			}
			// Add the first wildcard to the path
			wildPath = path.Join(wildPath, wild)

			if thisDepth > 0 {
				// Get the rest of the wildcards
				for j := 1; j <= thisDepth; j++ {
					wild, have := c.Params.Get(fmt.Sprintf("wild%d", j))
					if !have {
						slog.Error("Failed to get wildcard")
						return
					}
					wildPath = path.Join(wildPath, wild)
				}
			}
			file, fileErr := FS.Open(wildPath)
			if fileErr != nil {
				file, fileErr = FS.Open("frontend/dist/index.html")
				if fileErr != nil {
					slog.Error("Failed to open file", "error", fileErr)
					return
				}
			}
			defer func() {
				err := file.Close()
				if err != nil {
					slog.Error("Failed to close file", "error", err)
				}
			}()
			fileContent, readErr := io.ReadAll(file)
			if readErr != nil {
				slog.Error("Failed to read file", "error", readErr)
				return
			}
			c.Data(http.StatusOK, "text/html", fileContent)
		})
	}
}

func addFrontendRoutes(r *gin.Engine) {
	staticGroup := r.Group("/")

	files, depth, err := getAllFilenames(&FS, "frontend/dist")
	if err != nil {
		slog.Error("Failed to read directory", "error", err)
	}
	addFrontendWildcards(staticGroup, depth)
	for _, entry := range files {
		staticName := strings.Replace(entry, "frontend/dist", "", 1)
		if staticName == "" {
			continue
		}
		staticGroup.GET(staticName, func(c *gin.Context) {
			file, fileErr := FS.Open(fmt.Sprintf("frontend/dist%s", c.Request.URL.Path))
			if fileErr != nil {
				slog.Error("Failed to open file", "error", fileErr)
				return
			}
			defer func() {
				err = file.Close()
				if err != nil {
					slog.Error("Failed to close file", "error", err)
				}
			}()
			fileContent, fileErr := io.ReadAll(file)
			if fileErr != nil {
				slog.Error("Failed to read file", "error", fileErr)
				return
			}
			handleMime(c, fileContent, entry)
		})
	}
}

func handleMime(c *gin.Context, fileContent []byte, entry string) {
	switch {
	case strings.HasSuffix(c.Request.URL.Path, ".js"):
		c.Data(http.StatusOK, "text/javascript", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".css"):
		c.Data(http.StatusOK, "text/css", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".html") || strings.HasSuffix(entry, ".htm"):
		c.Data(http.StatusOK, "text/html", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".ico"):
		c.Data(http.StatusOK, "image/x-icon", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".png"):
		c.Data(http.StatusOK, "image/png", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".jpg") || strings.HasSuffix(entry, ".jpeg"):
		c.Data(http.StatusOK, "image/jpeg", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".webp"):
		c.Data(http.StatusOK, "image/webp", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".svg"):
		c.Data(http.StatusOK, "image/svg+xml", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".gif"):
		c.Data(http.StatusOK, "image/gif", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".json"):
		c.Data(http.StatusOK, "application/json", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".xml"):
		c.Data(http.StatusOK, "text/xml", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".txt"):
		c.Data(http.StatusOK, "text/plain", fileContent)
		return
	case strings.HasSuffix(c.Request.URL.Path, ".webmanifest"):
		c.Data(http.StatusOK, "application/manifest+json", fileContent)
		return
	default:
		c.Data(http.StatusOK, "text/plain", fileContent)
		return
	}
}

// getAllFilenames gets all filenames in a directory and its subdirectories
// It returns a slice of filenames, the maximum depth of the directory tree, and an error if any
// getAllFilenames gets all filenames in a directory and its subdirectories
// It returns a slice of filenames, the maximum depth of the directory tree, and an error if any
func getAllFilenames(fs *embed.FS, dir string) ([]string, int, error) {
	return getAllFilenamesDepth(fs, dir, 0)
}

// getAllFilenamesDepth is a helper that tracks the current depth
func getAllFilenamesDepth(fs *embed.FS, dir string, curDepth int) ([]string, int, error) {
	if len(dir) == 0 {
		dir = "."
	}

	entries, err := fs.ReadDir(dir)
	if err != nil {
		return nil, 0, ErrReadDir
	}

	out := []string{}
	maxDepth := curDepth

	for _, entry := range entries {
		fp := path.Join(dir, entry.Name())
		if entry.IsDir() {
			res, depth, err := getAllFilenamesDepth(fs, fp, curDepth+1)
			if err != nil {
				return nil, 0, err
			}
			out = append(out, res...)
			if depth > maxDepth {
				maxDepth = depth
			}
			continue
		}
		out = append(out, fp)
	}

	return out, maxDepth, nil
}
