package handler

import "draarl/pkg/geoip"

func getIPLocation(ip string) string {
	if ip == "" {
		return ""
	}
	return geoip.GetQTH(ip)
}
