package regexplib

import (
	"regexp"
)

type RegexpHost struct{}

func (h *RegexpHost) Match(pattern string, b []byte) (bool, error) {
	return regexp.Match(pattern, b)
}

func (h *RegexpHost) MatchString(pattern string, s string) (bool, error) {
	return regexp.MatchString(pattern, s)
}

func (h *RegexpHost) QuoteMeta(s string) string {
	return regexp.QuoteMeta(s)
}

func (h *RegexpHost) FindString(pattern string, s string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	return re.FindString(s)
}

func (h *RegexpHost) FindStringSubmatch(pattern string, s string) []string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re.FindStringSubmatch(s)
}

func (h *RegexpHost) ReplaceAllString(pattern string, src, repl string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	return re.ReplaceAllString(src, repl), nil
}

func (h *RegexpHost) Split(pattern string, s string, n int) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return re.Split(s, n), nil
}
