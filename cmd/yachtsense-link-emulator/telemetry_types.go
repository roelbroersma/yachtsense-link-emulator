package main

// Read-only router telemetry. Missing observations are never invented.
import "time"

type RouterView struct {
 Observed time.Time `json:"observed_at"`
 Firmware string `json:"firmware"`
 Profile string `json:"profile"`
 Internet InternetView `json:"internet"`
 WIFI []WifiView `json:"wifi"`
 Devices []DeviceView `json:"devices"`
 GPS GPSView `json:"gps"`
 VPN []VPNView `json:"vpn"`
 RMS string `json:"rms"`
 VXLAN []VXLANView `json:"vxlan"`
 Logs []string `json:"logs"`
 Sources map[string]string `json:"sources"`
}
type InternetView struct {Policy string `json:"policy"`;Source string `json:"source"`;State string `json:"state"`;Note string `json:"note"`;Links []UplinkView `json:"links"`}
type UplinkView struct {
 Name string `json:"name"`
 Device string `json:"device"`
 Kind string `json:"kind"`
 Label string `json:"label"`
 State string `json:"state"`
 Share *float64 `json:"share,omitempty"`
 RSSI *float64 `json:"rssi,omitempty"`
 RSRP *float64 `json:"rsrp,omitempty"`
 SINR *float64 `json:"sinr,omitempty"`
 SIM string `json:"sim,omitempty"`
 Carrier string `json:"carrier,omitempty"`
 Uptime *float64 `json:"uptime,omitempty"`
 IP string `json:"ip,omitempty"`
 PublicIP string `json:"public_ip,omitempty"`
 Present *bool `json:"present,omitempty"`
 Disabled bool `json:"disabled,omitempty"`
}
type WifiView struct {Name string `json:"name"`;SSID string `json:"ssid"`;Band string `json:"band"`;Channel *float64 `json:"channel,omitempty"`;Up bool `json:"up"`;Clients *int `json:"clients,omitempty"`}
type DeviceView struct {Name string `json:"name"`;IP string `json:"ip"`;MAC string `json:"mac"`;Interface string `json:"interface"`;Connection string `json:"connection"`;State string `json:"state"`;SSID string `json:"ssid,omitempty"`}
type GPSView struct {State string `json:"state"`;Latitude *float64 `json:"latitude,omitempty"`;Longitude *float64 `json:"longitude,omitempty"`;Satellites *float64 `json:"satellites,omitempty"`;Updated string `json:"updated,omitempty"`;Fences []FenceView `json:"fences"`;Note string `json:"note"`}
type FenceView struct {Name string `json:"name"`;State string `json:"state"`;Distance *float64 `json:"distance_m,omitempty"`;Radius float64 `json:"radius_m"`}
type VPNView struct {Name string `json:"name"`;Kind string `json:"kind"`;State string `json:"state"`;HandshakeAge *float64 `json:"handshake_age,omitempty"`;Uptime *float64 `json:"uptime,omitempty"`;RX *float64 `json:"rx_bytes,omitempty"`;TX *float64 `json:"tx_bytes,omitempty"`;Children int `json:"children,omitempty"`;Note string `json:"note,omitempty"`}
type VXLANView struct {Name string `json:"name"`;Up bool `json:"up"`;Operstate string `json:"operstate"`;VNI *float64 `json:"vni,omitempty"`;Remote string `json:"remote,omitempty"`;MTU *float64 `json:"mtu,omitempty"`;RX *float64 `json:"rx_bytes,omitempty"`;TX *float64 `json:"tx_bytes,omitempty"`;Note string `json:"note"`}
