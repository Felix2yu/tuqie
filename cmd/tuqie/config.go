// config.go turns the environment into flag defaults, so an instance can be
// tuned from a compose `environment:` block instead of rewriting the image's
// whole command line.

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// defaultMaxUpload is the ceiling for an instance that says nothing about size. It
// clears a phone photo by a wide margin and the 32MB PNG long screenshot the README
// measures by a narrow one, which is the pair of things actually handed to the
// tool. TUQIE_MAX_UPLOAD moves it; 0 takes it away.
const defaultMaxUpload = "35MB"

// envOr returns the environment variable a flag answers to: -max-upload reads
// TUQIE_MAX_UPLOAD. An unset or empty variable leaves the built-in default, and
// a flag on the command line beats both.
func envOr(flag, def string) string {
	key := "TUQIE_" + strings.ToUpper(strings.ReplaceAll(flag, "-", "_"))
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// units are the suffixes a size may carry, longest suffix first so that "kb" is
// not read as "k" plus a stray b. Every one of them is a multiple of 1024, so
// "15MB" means the same 15 << 20 the server compares the request against.
var units = []struct {
	suffix string
	factor float64
}{
	{"kb", 1 << 10}, {"k", 1 << 10},
	{"mb", 1 << 20}, {"m", 1 << 20},
	{"gb", 1 << 30}, {"g", 1 << 30},
	{"tb", 1 << 40}, {"t", 1 << 40},
	{"b", 1},
}

// parseSize reads a byte count with an optional unit, so 15728640, 15M, 15MB and
// 1.5G all mean what a person typing them means. 0 means no ceiling.
func parseSize(s string) (int64, error) {
	text := strings.ToLower(strings.TrimSpace(s))
	if text == "" {
		return 0, fmt.Errorf("空")
	}
	factor := 1.0
	for _, u := range units {
		if strings.HasSuffix(text, u.suffix) {
			factor = u.factor
			text = strings.TrimSuffix(text, u.suffix)
			break
		}
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return 0, fmt.Errorf("%q 不是一个大小", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("%q 是负数", s)
	}
	bytes := n * factor
	if bytes > 1<<62 {
		return 0, fmt.Errorf("%q 大得没有意义", s)
	}
	return int64(bytes), nil
}
