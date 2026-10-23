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

// Package positions serves the positions of the radios that the logged-in
// user tracks, and of the radios shared with them.
package positions

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	errorKey    = "error"
	msgTryLater = "Try again later"
	msgNotFound = "No positions for this radio"
	window      = 24 * time.Hour
)

// windowParam returns the time from which positions are shown: the hours
// query of 24, 168 or 720, a day by default.
func windowParam(c *gin.Context) (time.Time, bool) {
	hours := window
	if v := c.Query("hours"); v != "" {
		switch v {
		case "24", "168", "720":
			n, _ := strconv.Atoi(v)
			hours = time.Duration(n) * time.Hour
		default:
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "hours is 24, 168 or 720"})
			return time.Time{}, false
		}
	}
	return time.Now().Add(-hours), true
}

func failed(c *gin.Context, what string, err error) {
	slog.Error("Positions: "+what, "error", err)
	c.JSON(http.StatusInternalServerError, gin.H{errorKey: msgTryLater})
}

// GETPositions returns the newest position of each radio that the user
// sees, of the last 24 hours or the hours query.
func GETPositions(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	since, ok := windowParam(c)
	if !ok {
		return
	}
	latest, err := models.LatestPositions(db, user.ID, since)
	if err != nil {
		failed(c, "cannot list positions", err)
		return
	}
	out := apimodels.PositionList{Positions: make([]apimodels.Position, 0, len(latest))}
	for i := range latest {
		p, err := view(db, user.ID, &latest[i])
		if err != nil {
			failed(c, "cannot show a position", err)
			return
		}
		out.Positions = append(out.Positions, p)
	}
	c.JSON(http.StatusOK, out)
}

func view(db *gorm.DB, viewer uint, p *models.Position) (apimodels.Position, error) {
	out := apimodels.NewPosition(p)
	owner, ok, err := models.PositionOwner(db, p.RadioID)
	if err != nil {
		return out, fmt.Errorf("could not find the owner of %d: %w", p.RadioID, err)
	}
	if ok && owner != viewer {
		out.Shared = true
		var callsigns []string
		if err := db.Model(&models.User{}).Where("id = ?", owner).Limit(1).Pluck("callsign", &callsigns).Error; err != nil {
			return out, fmt.Errorf("could not find the callsign of %d: %w", owner, err)
		}
		if len(callsigns) == 1 {
			out.Callsign = callsigns[0]
		}
	}
	return out, nil
}

func radioParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "Invalid radio ID"})
		return 0, false
	}
	return uint(id), true
}

// GETRadioPositions returns the newest position and the track of one radio
// that the user sees, of the last 24 hours or the hours query.
func GETRadioPositions(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	radio, ok := radioParam(c)
	if !ok {
		return
	}
	since, ok := windowParam(c)
	if !ok {
		return
	}
	sees, err := models.SeesPositions(db, user.ID, radio)
	if err != nil {
		failed(c, "cannot check who sees a radio", err)
		return
	}
	if !sees {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotFound})
		return
	}
	track, err := models.Track(db, radio, since, time.Now())
	if err != nil {
		failed(c, "cannot read a track", err)
		return
	}
	out := apimodels.RadioPositions{Track: make([]apimodels.Position, 0, len(track))}
	for i := range track {
		p, err := view(db, user.ID, &track[i])
		if err != nil {
			failed(c, "cannot show a position", err)
			return
		}
		out.Track = append(out.Track, p)
	}
	if len(out.Track) > 0 {
		out.Latest = &out.Track[len(out.Track)-1]
	}
	c.JSON(http.StatusOK, out)
}

// GETTrack exports the track of a radio that the user tracks, as GPX,
// GeoJSON or CSV (format), between the times from and to (RFC 3339), at most the
// retention of the radio.
func GETTrack(c *gin.Context) {
	db, ok := utils.GetDB(c)
	if !ok {
		return
	}
	user, ok := utils.GetSessionUser(c, db)
	if !ok {
		return
	}
	radio, ok := radioParam(c)
	if !ok {
		return
	}
	format := c.DefaultQuery("format", "gpx")
	if format != "gpx" && format != "geojson" && format != "csv" {
		c.JSON(http.StatusBadRequest, gin.H{errorKey: "The format is gpx, geojson or csv"})
		return
	}
	owner, ok, err := models.PositionOwner(db, radio)
	if err != nil {
		failed(c, "cannot find the owner of a radio", err)
		return
	}
	if !ok || owner != user.ID {
		c.JSON(http.StatusNotFound, gin.H{errorKey: msgNotFound})
		return
	}
	to := time.Now()
	from := time.Time{}
	if v := c.Query("from"); v != "" {
		if from, err = time.Parse(time.RFC3339, v); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "from is an RFC 3339 time"})
			return
		}
	}
	if v := c.Query("to"); v != "" {
		if to, err = time.Parse(time.RFC3339, v); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{errorKey: "to is an RFC 3339 time"})
			return
		}
	}
	track, err := models.Track(db, radio, from, to)
	if err != nil {
		failed(c, "cannot read a track", err)
		return
	}
	name := fmt.Sprintf("track-%d.%s", radio, format)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	switch format {
	case "csv":
		writeCSV(c, track)
	case "geojson":
		writeGeoJSON(c, radio, track)
	default:
		writeGPX(c, radio, track)
	}
}

func optional(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func writeCSV(c *gin.Context, track []models.Position) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Status(http.StatusOK)
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"received_at", "fix_time", "latitude", "longitude", "accuracy_m", "speed_mps", "heading_deg", "altitude_m", "format", "repeater_id", "slot"})
	for _, p := range track {
		fix := ""
		if p.FixTime != nil {
			fix = p.FixTime.UTC().Format(time.RFC3339)
		}
		_ = w.Write([]string{
			p.ReceivedAt.UTC().Format(time.RFC3339), fix,
			strconv.FormatFloat(p.Latitude, 'f', -1, 64), strconv.FormatFloat(p.Longitude, 'f', -1, 64),
			optional(p.AccuracyM), optional(p.SpeedMPS), optional(p.HeadingDeg), optional(p.AltitudeM),
			string(p.Format), strconv.FormatUint(uint64(p.RepeaterID), 10), strconv.FormatUint(uint64(p.Slot), 10),
		})
	}
	w.Flush()
}

type geoFeature struct {
	Type       string         `json:"type"`
	Geometry   geoLine        `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

type geoLine struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"`
}

func writeGeoJSON(c *gin.Context, radio uint, track []models.Position) {
	line := geoLine{Type: "LineString", Coordinates: make([][]float64, 0, len(track))}
	times := make([]string, 0, len(track))
	for _, p := range track {
		line.Coordinates = append(line.Coordinates, []float64{p.Longitude, p.Latitude})
		times = append(times, p.ReceivedAt.UTC().Format(time.RFC3339))
	}
	doc := struct {
		Type     string       `json:"type"`
		Features []geoFeature `json:"features"`
	}{Type: "FeatureCollection", Features: []geoFeature{{
		Type: "Feature", Geometry: line, Properties: map[string]any{"radio_id": radio, "times": times},
	}}}
	data, err := json.Marshal(doc)
	if err != nil {
		failed(c, "cannot encode a track", err)
		return
	}
	c.Data(http.StatusOK, "application/geo+json", data)
}

type gpxDoc struct {
	XMLName xml.Name `xml:"gpx"`
	Version string   `xml:"version,attr"`
	Creator string   `xml:"creator,attr"`
	NS      string   `xml:"xmlns,attr"`
	Track   gpxTrack `xml:"trk"`
}

type gpxTrack struct {
	Name    string     `xml:"name"`
	Segment gpxSegment `xml:"trkseg"`
}

type gpxSegment struct {
	Points []gpxPoint `xml:"trkpt"`
}

type gpxPoint struct {
	Lat  float64  `xml:"lat,attr"`
	Lon  float64  `xml:"lon,attr"`
	Ele  *float64 `xml:"ele,omitempty"`
	Time string   `xml:"time"`
}

func writeGPX(c *gin.Context, radio uint, track []models.Position) {
	doc := gpxDoc{Version: "1.1", Creator: "DMRHub", NS: "http://www.topografix.com/GPX/1/1",
		Track: gpxTrack{Name: strconv.FormatUint(uint64(radio), 10)}}
	for _, p := range track {
		doc.Track.Segment.Points = append(doc.Track.Segment.Points, gpxPoint{
			Lat: p.Latitude, Lon: p.Longitude, Ele: p.AltitudeM, Time: p.ReceivedAt.UTC().Format(time.RFC3339),
		})
	}
	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		failed(c, "cannot encode a track", err)
		return
	}
	c.Data(http.StatusOK, "application/gpx+xml", append([]byte(xml.Header), data...))
}
