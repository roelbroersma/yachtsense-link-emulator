package main

// Read-only router telemetry. Missing observations are never invented.
import (
	"math"
	"strings"
	"time"
)

func nestedCoordinate(v any) map[string]any {
	for _, m := range rows(v) {
		if num(m, "latitude", "lat") != nil && num(m, "longitude", "lon", "lng") != nil {
			return m
		}
		for _, k := range []string{"position", "gps", "data", "location"} {
			if x := obj(m[k]); x != nil {
				if y := nestedCoordinate(x); y != nil {
					return y
				}
			}
		}
	}
	return nil
}
func gpsView(position, fix, fences any, now time.Time) GPSView {
	out := GPSView{State: "unknown", Fences: []FenceView{}, Note: "Geofence membership is calculated from the current GPS fix; no profile or event is changed."}
	m := nestedCoordinate(position)
	if m == nil {
		m = nestedCoordinate(fix)
	}
	lat, lon := num(m, "latitude", "lat"), num(m, "longitude", "lon", "lng")
	valid, known := truth(m["fix"])
	if !known {
		valid, known = truth(m["fix_status"])
	}
	if n := num(m, "mode", "fix_type"); n != nil {
		known = true
		valid = *n >= 2
	}
	if n := num(m, "fix_status"); n != nil && !known {
		known = true
		valid = *n > 0
	}
	if st := strings.ToLower(first(m, "fix", "fix_status")); st == "2d" || st == "3d" || st == "valid" {
		valid = true
		known = true
	}
	if !known {
		fm := obj(fix)
		valid, known = truth(fm["fix"])
		if n := num(fm, "mode", "fix_type"); n != nil {
			known = true
			valid = *n >= 2
		}
	}
	if !known {
		fm := obj(fix)
		if q := num(fm, "fix_status", "fix", "status"); q != nil {
			known = true
			valid = *q > 0
		}
	}
	ts := first(m, "utc_timestamp", "timestamp", "time", "updated_at")
	out.Updated = ts
	if ts != "" {
		t, e := time.Parse(time.RFC3339, ts)
		if e != nil {
			if sec, ok := number(ts); ok {
				t = time.Unix(int64(sec), 0)
				e = nil
			}
		}
		if e == nil {
			age := now.Sub(t)
			if age > 45*time.Second || age < -5*time.Second {
				valid = false
				known = true
				out.State = "stale"
			} else if !known {
				valid = true
				known = true
			}
		}
	}
	if known && !valid && out.State != "stale" {
		out.State = "no_fix"
	}
	if lat != nil && lon != nil && math.Abs(*lat) <= 90 && math.Abs(*lon) <= 180 && known && valid {
		out.State = "fix"
		out.Latitude = lat
		out.Longitude = lon
		out.Satellites = num(m, "satellites", "satellites_used", "sats")
	}
	for _, f := range rows(fences) {
		la, lo, rad := num(f, "latitude", "lat", "y"), num(f, "longitude", "lon", "lng", "x"), num(f, "radius")
		if la == nil || lo == nil || rad == nil || *rad <= 0 || *rad > 999999 {
			continue
		}
		n := first(f, "name", "id")
		fv := FenceView{Name: n, Radius: *rad, State: "unknown"}
		enabled, ok := truth(f["enabled"])
		if !ok {
			enabled, ok = truth(f["enable"])
		}
		if ok && !enabled {
			fv.State = "disabled"
		} else if out.State == "fix" && ok && enabled {
			d := distanceMeters(*lat, *lon, *la, *lo)
			fv.Distance = &d
			fv.State = "outside"
			if d <= *rad {
				fv.State = "inside"
			}
		}
		out.Fences = append(out.Fences, fv)
		if len(out.Fences) >= 64 {
			break
		}
	}
	return out
}
func distanceMeters(a, b, c, d float64) float64 {
	rad := math.Pi / 180
	x := (c - a) * rad
	y := (d - b) * rad
	v := math.Sin(x/2)*math.Sin(x/2) + math.Cos(a*rad)*math.Cos(c*rad)*math.Sin(y/2)*math.Sin(y/2)
	return 6371000 * 2 * math.Atan2(math.Sqrt(v), math.Sqrt(math.Max(0, 1-v)))
}
