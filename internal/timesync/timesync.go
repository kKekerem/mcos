// Package timesync sets the system clock from NTP. Many of the PCs MCOS runs on
// have no RTC battery, so they boot with a wrong (often 1970/2010) date. TLS
// certificate validation then fails and *every* HTTPS download (Java, Mojang,
// mod catalogs) breaks before it starts. NTP runs over plain UDP and needs no
// certificates, so it works even with a wildly wrong clock — making it the one
// thing that can bootstrap a correct time on a battery-less box.
//
// The query logic is cross-platform; only writing the clock is OS-specific
// (Linux uses settimeofday + hwclock, dev hosts are a safe no-op).
package timesync

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync/atomic"
	"time"
)

// Servers are queried in order until one answers. All are anycast/public NTP.
var Servers = []string{
	"pool.ntp.org",
	"time.google.com",
	"time.cloudflare.com",
	"time.windows.com",
}

// ntpEpochOffset is the seconds between the NTP epoch (1900) and Unix (1970).
const ntpEpochOffset = 2208988800

// minSaneYear is the lowest year we consider a "correct" clock. A box booting
// below this almost certainly has a dead RTC and needs a sync.
const minSaneYear = 2024

var synced atomic.Bool

// Synced reports whether the clock has been set from NTP in this session.
func Synced() bool { return synced.Load() }

// NeedsSync reports whether the current clock looks implausible (pre-2024),
// which on an RTC-less PC means it must be corrected before any TLS download.
func NeedsSync() bool { return time.Now().Year() < minSaneYear }

// Sync queries NTP and, when the clock is implausible (or force is set), writes
// the result to the system clock. It returns the network time it obtained even
// when it chooses not to write it. Safe to call repeatedly.
func Sync(force bool) (time.Time, error) {
	netTime, err := Query()
	if err != nil {
		return time.Time{}, err
	}
	if force || NeedsSync() {
		if err := setSystemClock(netTime); err != nil {
			return netTime, fmt.Errorf("timesync: set clock: %w", err)
		}
	}
	synced.Store(true)
	return netTime, nil
}

// Query asks each NTP server in turn and returns the first valid response.
func Query() (time.Time, error) {
	var lastErr error
	for _, s := range Servers {
		t, err := queryNTP(s, 5*time.Second)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no NTP servers configured")
	}
	return time.Time{}, fmt.Errorf("timesync: query failed: %w", lastErr)
}

// queryNTP performs a single SNTPv3 request and parses the transmit timestamp.
func queryNTP(host string, timeout time.Duration) (time.Time, error) {
	conn, err := net.DialTimeout("udp", net.JoinHostPort(host, "123"), timeout)
	if err != nil {
		return time.Time{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	// 48-byte SNTP request. First byte: LI=0, VN=3, Mode=3 (client) = 0x1B.
	req := make([]byte, 48)
	req[0] = 0x1B
	if _, err := conn.Write(req); err != nil {
		return time.Time{}, err
	}

	resp := make([]byte, 48)
	if _, err := conn.Read(resp); err != nil {
		return time.Time{}, err
	}

	// Transmit timestamp: seconds (bytes 40-43) + fraction (44-47), NTP epoch.
	secs := binary.BigEndian.Uint32(resp[40:44])
	frac := binary.BigEndian.Uint32(resp[44:48])
	if secs == 0 {
		return time.Time{}, fmt.Errorf("empty NTP timestamp")
	}
	unixSec := int64(secs) - ntpEpochOffset
	nanos := (int64(frac) * 1e9) >> 32
	return time.Unix(unixSec, nanos).UTC(), nil
}
