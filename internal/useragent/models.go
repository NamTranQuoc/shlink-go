package useragent

// DeviceType is Shlink's Core\Model\DeviceType, used by device redirect conditions.
type DeviceType string

const (
	DeviceTypeAndroid  DeviceType = "android"
	DeviceTypeIOS      DeviceType = "ios"
	DeviceTypeMobile   DeviceType = "mobile"
	DeviceTypeWindows  DeviceType = "windows"
	DeviceTypeMacOS    DeviceType = "macos"
	DeviceTypeLinux    DeviceType = "linux"
	DeviceTypeChromeOS DeviceType = "chromeos"
	DeviceTypeDesktop  DeviceType = "desktop"
)

// DeviceTypesFromUserAgent is DeviceType::matchFromUserAgent: every device type the user agent
// matches, possibly none.
func DeviceTypesFromUserAgent(userAgent string) []DeviceType {
	switch Parse(userAgent).Platform {
	case PlatformIPhone, PlatformIPad: // iPhone and iPad (except iPadOS 13+)
		return []DeviceType{DeviceTypeIOS, DeviceTypeMobile}
	case PlatformAndroid: // android phones and android tablets
		return []DeviceType{DeviceTypeAndroid, DeviceTypeMobile}
	case PlatformLinux:
		return []DeviceType{DeviceTypeLinux, DeviceTypeDesktop}
	case PlatformWindows:
		return []DeviceType{DeviceTypeWindows, DeviceTypeDesktop}
	case PlatformMacintosh:
		return []DeviceType{DeviceTypeMacOS, DeviceTypeDesktop}
	case PlatformChromeOS:
		return []DeviceType{DeviceTypeChromeOS, DeviceTypeDesktop}
	}
	return nil
}

// Browser is Shlink's Core\Model\Browser, used by browser redirect conditions.
type Browser string

const (
	BrowserChrome         Browser = "chrome"
	BrowserFirefox        Browser = "firefox"
	BrowserEdge           Browser = "edge"
	BrowserSafari         Browser = "safari"
	BrowserOpera          Browser = "opera"
	BrowserAndroidBrowser Browser = "android_browser"
)

// BrowserFromUserAgent is Browser::matchFromUserAgent; false stands for its null.
func BrowserFromUserAgent(userAgent string) (Browser, bool) {
	switch Parse(userAgent).Browser {
	case BrowserNameChrome:
		return BrowserChrome, true
	case BrowserNameFirefox:
		return BrowserFirefox, true
	case BrowserNameEdge:
		return BrowserEdge, true
	case BrowserNameSafari:
		return BrowserSafari, true
	case BrowserNameOpera:
		return BrowserOpera, true
	case BrowserNameAndroidBrowser:
		return BrowserAndroidBrowser, true
	}
	return "", false
}
