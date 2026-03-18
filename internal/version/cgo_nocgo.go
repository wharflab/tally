//go:build !cgo

package version

func cgoInfo() CGOInfo {
	return CGOInfo{Enabled: false}
}
