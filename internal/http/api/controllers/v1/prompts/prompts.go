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

// Package prompts serves the spoken prompts: the voice sets and the clips,
// and a preview of each clip that a browser can play.
package prompts

import (
	"errors"
	"net/http"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/prompts"
	"github.com/gin-gonic/gin"
)

const errorKey = "error"

// Clip is one clip of the voice sets.
type Clip struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// List is the voice sets, the clips, and the voice of the hub for users
// who did not pick one.
type List struct {
	Voices  []prompts.Voice `json:"voices"`
	Clips   []Clip          `json:"clips"`
	Default string          `json:"default"`
}

// DefaultVoice returns the voice of the prompts of the hub with cfg for
// users who did not pick one.
func DefaultVoice(cfg *config.Config) string {
	if cfg != nil && prompts.HasVoice(cfg.DMR.Routing.PromptVoice) {
		return cfg.DMR.Routing.PromptVoice
	}
	return prompts.DefaultVoice
}

func configOf(c *gin.Context) *config.Config {
	if v, ok := c.Get("Config"); ok {
		if cfg, ok := v.(*config.Config); ok {
			return cfg
		}
	}
	return nil
}

// GETVoices lists the voice sets and the clips.
func GETVoices(c *gin.Context) {
	clips := prompts.Clips()
	out := List{Voices: prompts.Voices(), Clips: make([]Clip, 0, len(clips)), Default: DefaultVoice(configOf(c))}
	for _, name := range clips {
		out.Clips = append(out.Clips, Clip{Name: name, Text: prompts.Text(name)})
	}
	c.JSON(http.StatusOK, out)
}

// GETPreview serves the clip :file, NAME.wav, of the voice set :voice.
func GETPreview(c *gin.Context) {
	serve(c, c.Param("voice"), c.Param("file"))
}

// GETConfigPreview serves the clip :file, NAME.wav, in the voice set of
// the query parameter voice, or the default voice of the hub without one.
func GETConfigPreview(c *gin.Context) {
	voice := c.Query("voice")
	if voice == "" {
		voice = DefaultVoice(configOf(c))
	}
	serve(c, voice, c.Param("file"))
}

func serve(c *gin.Context, voice, file string) {
	clip, ok := strings.CutSuffix(file, ".wav")
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{errorKey: "Prompt not found"})
		return
	}
	b, err := prompts.Preview(voice, clip)
	if errors.Is(err, prompts.ErrUnknown) {
		c.JSON(http.StatusNotFound, gin.H{errorKey: "Prompt not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{errorKey: "Try again later"})
		return
	}
	c.Header("Cache-Control", "private, max-age=86400")
	c.Data(http.StatusOK, "audio/wav", b)
}
